// Package grouping owns the grouping file, .verilex/grouping.yaml: the record of every
// onboarding decision, which word joined the vocabulary, under which claim, and for which
// products it is useful. Onboarding makes the decisions and is the only writer; everything else
// reads. Each decision carries a seal over its own fields, so a hand edit voids that decision
// instead of admitting anything, while decisions about different words merge as plain lines.
package grouping

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/DereKk8/verilex/internal/dictionary"
	"go.yaml.in/yaml/v3"
)

// sealFormat changes whenever the meaning of a seal changes, so older seals stop matching.
const sealFormat = "verilex-grouping-1"

const header = "# verilex grouping: written only by `verilex onboard`. Each decision says which claim a word is\n# grouped under and for which products it is useful. A decision edited by hand no longer\n# matches its seal, and verilex ignores it until the word is onboarded again.\n"

// How a word was grouped under its claim.
const (
	// Same: the word pins a claim the vocabulary already groups.
	Same = "same"
	// Mechanical: the word's claim matched a grouped claim mechanically and behaved the same.
	Mechanical = "mechanical"
	// Agent: the word's claim stayed ambiguous after the mechanical and behavioral checks, and
	// the calling agent decided it says the same as a grouped claim.
	Agent = "agent"
	// New: the word's claim matched no grouped claim, so it joined as a claim of its own.
	New = "new"
)

// Decision is one onboarding decision: a word joined the vocabulary.
type Decision struct {
	// Claim is the version of the grouped claim the word belongs to, <claim>@<version>.
	Claim string `yaml:"claim" json:"claim"`
	// Proves is the claim version the word pins when it differs from Claim: the word's own claim
	// is an alias of Claim.
	Proves string `yaml:"proves,omitempty" json:"proves,omitempty"`
	// Entry is the user entry point the word exercises.
	Entry string `yaml:"entry" json:"entry"`
	// Digest fingerprints the word's files as onboarding judged them.
	Digest string `yaml:"digest" json:"digest"`
	// Match says how the word was grouped: Same, Mechanical, Agent or New.
	Match string `yaml:"match" json:"match"`
	// Pending lists the grouped claims a New word's claim may say the same as, which only the
	// calling agent can decide; the word stays a claim of its own until it does.
	Pending []Pending `yaml:"pending,omitempty" json:"pending,omitempty"`
	// Defects maps each planted defect the word caught to the digest of its definition.
	Defects map[string]string `yaml:"defects" json:"defects"`
	// GroupDefects maps each planted defect of the grouped claim to its digest, for a word whose
	// own claim is an alias: the word behaved like the grouped claim's words under each of them.
	GroupDefects map[string]string `yaml:"group_defects,omitempty" json:"group_defects,omitempty"`
	// Uses maps each product the word is useful for to the runs that counted there.
	Uses map[string][]string `yaml:"uses" json:"uses"`
	// Chain is the chain onboarding proved the word on: the states of a real use, ending in it.
	Chain string `yaml:"chain" json:"chain"`
	// ClaimSources fingerprints the sources of the word's own claim as onboarding checked its
	// mapping (dictionary.Claim.SourcesDigest); Sources records each of them that lives in
	// another repository, at the commit onboarding checked.
	ClaimSources string   `yaml:"claim_sources" json:"claim_sources"`
	Sources      []Source `yaml:"sources,omitempty" json:"sources,omitempty"`
	Date         string   `yaml:"date" json:"date"`
	// Record is the id of the onboarding record, with every trial's evidence, in the state directory.
	Record string `yaml:"record" json:"record"`
	Seal   string `yaml:"seal" json:"-"`
}

// Pending is a grouped claim, at the version onboarding compared, that a word's claim may say the
// same as. Words are its words the behavioral check compared the word with, and Defects holds the
// digest of each of its planted defects the word behaved like them under.
type Pending struct {
	Claim   string            `yaml:"claim" json:"claim"`
	Words   []string          `yaml:"words,omitempty" json:"words,omitempty"`
	Defects map[string]string `yaml:"defects" json:"defects"`
}

// Source is a claim source in another repository, pinned to the commit onboarding checked.
type Source struct {
	Ref    string `yaml:"ref" json:"ref"`
	Repo   string `yaml:"repo" json:"repo"`
	Commit string `yaml:"commit" json:"commit"`
}

// Pin is the claim version the word pins: Proves for an alias, otherwise Claim.
func (d Decision) Pin() string {
	if d.Proves != "" {
		return d.Proves
	}
	return d.Claim
}

// Products lists the products the word is useful for, sorted.
func (d Decision) Products() []string {
	products := make([]string, 0, len(d.Uses))
	for product := range d.Uses {
		products = append(products, product)
	}
	slices.Sort(products)
	return products
}

// Grouping is the content of a project's grouping file.
type Grouping struct {
	Words map[string]Decision `yaml:"words"`
}

// Path is where a project's grouping file lives.
func Path(p dictionary.Project) string { return filepath.Join(p.Dir(), "grouping.yaml") }

type parsed struct {
	data []byte
	g    Grouping
	err  error
}

var (
	mu    sync.Mutex
	cache = map[string]parsed{}
)

// Load reads a project's grouping file; a project without one has no decisions yet. Unknown
// fields are refused, so a misspelled field cannot slip past its seal.
func Load(p dictionary.Project) (Grouping, error) {
	path := Path(p)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Grouping{Words: map[string]Decision{}}, nil
	}
	if err != nil {
		return Grouping{}, err
	}
	mu.Lock()
	defer mu.Unlock()
	if c, ok := cache[path]; ok && bytes.Equal(c.data, data) {
		return c.g.clone(), c.err
	}
	g, err := parse(path, data)
	cache[path] = parsed{data, g, err}
	return g.clone(), err
}

func parse(path string, data []byte) (Grouping, error) {
	var g Grouping
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&g); err != nil && !errors.Is(err, io.EOF) {
		return Grouping{}, fmt.Errorf("%s: malformed grouping file: %v", path, err)
	}
	if g.Words == nil {
		g.Words = map[string]Decision{}
	}
	return g, nil
}

// clone copies every decision, so a caller that changes one never changes the cached file.
func (g Grouping) clone() Grouping {
	words := make(map[string]Decision, len(g.Words))
	for name, d := range g.Words {
		words[name] = d.normal()
	}
	return Grouping{Words: words}
}

// normal copies d with every map and list present and owned, so a decision seals the same before
// and after a trip through YAML.
func (d Decision) normal() Decision {
	defects := map[string]string{}
	for name, digest := range d.Defects {
		defects[name] = digest
	}
	uses := map[string][]string{}
	for product, runs := range d.Uses {
		uses[product] = append([]string{}, runs...)
	}
	d.Defects, d.Uses, d.GroupDefects = defects, uses, maps.Clone(d.GroupDefects)
	if len(d.Pending) == 0 {
		d.Pending = nil
	} else {
		pending := make([]Pending, len(d.Pending))
		for i, p := range d.Pending {
			pending[i] = Pending{Claim: p.Claim, Words: slices.Clone(p.Words), Defects: maps.Clone(p.Defects)}
		}
		d.Pending = pending
	}
	if len(d.Sources) == 0 {
		d.Sources = nil
	} else {
		d.Sources = slices.Clone(d.Sources)
	}
	return d
}

// Broken says why a decision cannot be trusted: it does not match its seal, so it was written or
// changed outside onboarding. It is empty for a sound decision.
func Broken(word string, d Decision) string {
	if d.Seal != seal(word, d) {
		return "its grouping decision does not match its seal: it was edited outside `verilex onboard`"
	}
	return ""
}

// Sound returns the word's decision when the grouping holds one that matches its seal.
func (g Grouping) Sound(word string) (Decision, bool) {
	d, ok := g.Words[word]
	return d, ok && Broken(word, d) == ""
}

// Groups maps each claim name a sound decision places to the name of the claim it is grouped
// under: a grouped claim maps to itself, an alias to its grouped claim. Decisions are read in word
// order, and the first to place a claim wins, so the map depends only on the file. except names a
// word whose decision is left out, the one being onboarded.
func (g Grouping) Groups(except string) map[string]string {
	group := map[string]string{}
	for _, word := range slices.Sorted(maps.Keys(g.Words)) {
		d, ok := g.Sound(word)
		if !ok || word == except {
			continue
		}
		if _, placed := group[Name(d.Claim)]; !placed {
			group[Name(d.Claim)] = Name(d.Claim)
		}
		if _, placed := group[Name(d.Proves)]; d.Proves != "" && !placed {
			group[Name(d.Proves)] = Name(d.Claim)
		}
	}
	return group
}

// Open lists the grouping questions still open for a grouped claim: the claims, at the version
// compared, that the words grouped under it may say the same as, sorted, each with the words
// whose decisions leave it pending. except names a word whose decision is left out.
func (g Grouping) Open(claim, except string) map[string][]string {
	open := map[string][]string{}
	for _, word := range slices.Sorted(maps.Keys(g.Words)) {
		d, ok := g.Sound(word)
		if !ok || word == except || Name(d.Claim) != claim {
			continue
		}
		for _, pending := range d.Pending {
			open[pending.Claim] = append(open[pending.Claim], word)
		}
	}
	return open
}

// Set records a decision for a word, sealed. Only onboarding calls it.
func (g *Grouping) Set(word string, d Decision) {
	d = d.normal()
	d.Seal = seal(word, d)
	g.Words[word] = d
}

func seal(word string, d Decision) string {
	data, _ := json.Marshal(d.normal())
	sum := sha256.Sum256([]byte(sealFormat + "\n" + word + "\n" + string(data)))
	return hex.EncodeToString(sum[:])
}

// Save writes the grouping file atomically, every decision in name order, so two onboardings of
// different words change different lines.
func Save(p dictionary.Project, g Grouping) error {
	var body bytes.Buffer
	encoder := yaml.NewEncoder(&body)
	encoder.SetIndent(2)
	if err := encoder.Encode(g); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	path := Path(p)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(header+body.String()), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Update changes the grouping file under an exclusive lock, reading it afresh first, so two
// onboardings of different words at once never lose each other's decision.
func Update(p dictionary.Project, change func(*Grouping)) error {
	unlock, err := lock(p.Dir())
	if err != nil {
		return err
	}
	defer unlock()
	g, err := Load(p)
	if err != nil {
		return err
	}
	change(&g)
	return Save(p, g)
}

// Name is the claim name of a pin, <claim>@<version>.
func Name(pin string) string {
	name, _, _ := strings.Cut(pin, "@")
	return name
}
