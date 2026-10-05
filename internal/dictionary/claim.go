package dictionary

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// claimFormat changes whenever the meaning of a claim fingerprint changes.
const claimFormat = "verilex-claim-1"

// versionLength is how many hex digits of the fingerprint name a claim version.
const versionLength = 12

// Claim is what is true once a word that proves it passes, and the evidence that shows it.
// Several words (variants) may prove one claim. Its identity is a fingerprint of everything
// that gives it meaning; any change to that is a new version, and a pass is evidence only for
// the version it ran against. Its sources pin where the verify skill states it; they are not
// identity: a change there asks for a review, not a new version.
type Claim struct {
	Name, Path string
	// Sentence is what is true once the claim holds.
	Sentence string
	// Args name the placeholders its states use; a word that proves it takes them all.
	Args []string
	// Entry lists the user entry points through which it can be proven; a pass through one
	// never covers another.
	Entry []string
	// Preconditions are the configuration, build and environment postures it holds under.
	Preconditions []string
	// Requires and Provides pin the order of reality for every word that proves the claim:
	// a chain that breaks them is refused before it starts.
	Requires, Provides []string
	Evidence           Evidence
	Sources            []ClaimSource
	// Fingerprint covers Sentence, Args, Entry, Preconditions, Requires, Provides and Evidence.
	Fingerprint string
}

// Evidence is a claim's evidence contract.
type Evidence struct {
	// Action is what the word does and what the action record (command, output, exit code) must show.
	Action string `yaml:"action" json:"action"`
	// Observation is the independent, read-only second look at the resulting state.
	Observation string `yaml:"observation" json:"observation"`
	// NonProofs look like success but do not prove the claim.
	NonProofs []string `yaml:"non_proofs" json:"non_proofs"`
}

// ClaimSource anchors a claim in the verify skill: a sub-feature reference and the requirement
// sentences there that the claim maps to.
type ClaimSource struct {
	Ref          string   `yaml:"ref" json:"ref"`
	Requirements []string `yaml:"requirements" json:"requirements"`
	// Covered lists what every claim of the project maps in the same sub-feature. A requirement
	// sentence of the sub-feature outside it is one no claim proves yet. LoadClaims fills it.
	Covered []string `yaml:"-" json:"-"`
}

// Version names the claim's current version: the first hex digits of its fingerprint.
func (c Claim) Version() string { return c.Fingerprint[:versionLength] }

// Pin is how a word binds the claim's current version: <claim>@<version>.
func (c Claim) Pin() string { return c.Name + "@" + c.Version() }

// Refs lists the verify-skill references the claim's sources point at.
func (c Claim) Refs() []string {
	refs := make([]string, 0, len(c.Sources))
	for _, source := range c.Sources {
		refs = append(refs, source.Ref)
	}
	return refs
}

// ClaimsDir holds a project's claims, one <claim>.yaml each.
func (p Project) ClaimsDir() string { return filepath.Join(p.Dir(), "claims") }

// LoadClaims reads every claim of a project, keyed by name.
func LoadClaims(p Project) (map[string]Claim, error) {
	paths, err := filepath.Glob(filepath.Join(p.ClaimsDir(), "*.yaml"))
	if err != nil {
		return nil, err
	}
	claims := map[string]Claim{}
	for _, path := range paths {
		c, err := ReadClaim(path)
		if err != nil {
			return nil, err
		}
		claims[c.Name] = c
	}
	covered := map[string][]string{}
	for _, c := range claims {
		for _, source := range c.Sources {
			covered[subFeature(source.Ref)] = append(covered[subFeature(source.Ref)], source.Requirements...)
		}
	}
	for _, c := range claims {
		for i := range c.Sources {
			c.Sources[i].Covered = covered[subFeature(c.Sources[i].Ref)]
		}
	}
	return claims, nil
}

// subFeature names the sub-feature a source reference points at, however its path is spelled.
func subFeature(ref string) string {
	file, id, _ := strings.Cut(ref, "#")
	return path.Clean(filepath.ToSlash(file)) + "#" + id
}

type claimFile struct {
	Claim         string        `yaml:"claim"`
	Sentence      string        `yaml:"sentence"`
	Args          []string      `yaml:"args"`
	Entry         []string      `yaml:"entry"`
	Preconditions []string      `yaml:"preconditions"`
	Requires      []string      `yaml:"requires"`
	Provides      []string      `yaml:"provides"`
	Evidence      Evidence      `yaml:"evidence"`
	Sources       []ClaimSource `yaml:"sources"`
}

var (
	claimPin = regexp.MustCompile(`^([\pL\pN][\pL\pN._-]*)@(\S+)$`)
	argName  = regexp.MustCompile(`^[\pL\pN_]+$`)
)

// ReadClaim reads and checks one claim file and fingerprints its identity. Unknown fields are
// refused: a misspelled field would silently drop out of the fingerprint.
func ReadClaim(path string) (Claim, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Claim{}, err
	}
	var f claimFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return Claim{}, fmt.Errorf("%s: malformed claim: %v", path, err)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".yaml")
	c := Claim{
		Name: f.Claim, Path: path, Sentence: collapse(f.Sentence),
		Args: set(f.Args), Entry: set(f.Entry), Preconditions: set(f.Preconditions), Requires: set(f.Requires), Provides: set(f.Provides),
		Evidence: Evidence{collapse(f.Evidence.Action), collapse(f.Evidence.Observation), set(f.Evidence.NonProofs)},
		Sources:  f.Sources,
	}
	switch {
	case c.Name != name || !WordName.MatchString(name):
		return c, fmt.Errorf("%s: 'claim' must equal the file name %s", path, quote(name))
	case c.Sentence == "":
		return c, fmt.Errorf("%s: 'sentence' must say what is true once the claim holds", path)
	case len(c.Entry) == 0:
		return c, fmt.Errorf("%s: 'entry' must list the user entry points that can prove the claim", path)
	case c.Evidence.Action == "" || c.Evidence.Observation == "":
		return c, fmt.Errorf("%s: 'evidence' must name the 'action' and an independent 'observation'", path)
	case len(c.Sources) == 0:
		return c, fmt.Errorf("%s: 'sources' must anchor the claim in the verify skill", path)
	}
	for _, entry := range c.Entry {
		if !WordName.MatchString(entry) {
			return c, fmt.Errorf("%s: entry point %s is not a plain name", path, quote(entry))
		}
	}
	for _, arg := range c.Args {
		if !argName.MatchString(arg) {
			return c, fmt.Errorf("%s: arg %s is not a placeholder name", path, quote(arg))
		}
	}
	for _, source := range c.Sources {
		if !strings.Contains(source.Ref, "#") || len(set(source.Requirements)) == 0 {
			return c, fmt.Errorf("%s: each source needs a 'ref' <skill>/<file>#<sub-feature> and the 'requirements' sentences the claim maps to", path)
		}
	}
	if err = undeclared(path, append(slices.Clone(c.Requires), c.Provides...), c.Args); err != nil {
		return c, err
	}
	identity, err := json.Marshal(struct {
		Sentence      string   `json:"sentence"`
		Args          []string `json:"args"`
		Entry         []string `json:"entry"`
		Preconditions []string `json:"preconditions"`
		Requires      []string `json:"requires"`
		Provides      []string `json:"provides"`
		Evidence      Evidence `json:"evidence"`
	}{c.Sentence, c.Args, c.Entry, c.Preconditions, c.Requires, c.Provides, c.Evidence})
	if err != nil {
		return c, err
	}
	sum := sha256.Sum256([]byte(claimFormat + "\n" + string(identity)))
	c.Fingerprint = hex.EncodeToString(sum[:])
	return c, nil
}

// bindClaim makes w prove the claim its contract pins, which must exist. The claim's states
// join the word's own, so no variant can drop the order of reality the claim pins. A pin that
// names an older version leaves the word stale: a chain that holds it is refused.
func bindClaim(w *Word, file string, pin string, claims map[string]Claim) error {
	m := claimPin.FindStringSubmatch(pin)
	if m == nil {
		return fmt.Errorf("%s: 'claim' must pin a claim version, <claim>@<version> as `verilex claims` prints it", file)
	}
	c, ok := claims[m[1]]
	switch {
	case !ok:
		return fmt.Errorf("%s: no claim %s in .verilex/claims", file, quote(m[1]))
	case len(w.Implements) > 0:
		return fmt.Errorf("%s: a word that proves a claim takes its feature-map sources from the claim; drop 'implements'", file)
	case !slices.Contains(c.Entry, w.Entry):
		return fmt.Errorf("%s: 'entry' must name the entry point the word exercises, one of claim %s's: %s", file, c.Name, strings.Join(c.Entry, ", "))
	case w.ReadOnly && len(c.Provides) > 0:
		return fmt.Errorf("%s: a 'read_only' word changes nothing, so it cannot prove claim %s, which provides states", file, c.Name)
	}
	for _, arg := range c.Args {
		if !slices.Contains(w.Args, arg) {
			return fmt.Errorf("%s: claim %s uses {%s}, so the word must take arg %s", file, c.Name, arg, quote(arg))
		}
	}
	w.Claim, w.Pinned, w.pin = &c, c.Requires, pin
	w.Implements = c.Refs()
	w.Requires = union(w.Requires, c.Requires)
	w.Provides = union(w.Provides, c.Provides)
	if m[2] != c.Version() {
		w.Stale = fmt.Sprintf("%s pins claim %s, which is now %s; a pass proves only the version it ran against: check that %s still proves the claim, then pin %s", w.Name, pin, c.Pin(), w.Name, c.Pin())
	}
	return nil
}

// undeclared refuses states whose placeholders name no declared arg.
func undeclared(file string, states, args []string) error {
	for _, state := range states {
		unknown := []string{}
		for _, match := range placeholder.FindAllStringSubmatch(state, -1) {
			if !slices.Contains(args, match[1]) && !slices.Contains(unknown, match[1]) {
				unknown = append(unknown, match[1])
			}
		}
		if len(unknown) > 0 {
			slices.Sort(unknown)
			return fmt.Errorf("%s: state %s uses undeclared args %s", file, quote(state), repr(unknown))
		}
	}
	return nil
}

// set collapses whitespace in each entry, drops empty ones and duplicates, and sorts: order and
// layout never change what a list means.
func set(values []string) []string {
	result := []string{}
	for _, v := range values {
		if v = collapse(v); v != "" && !slices.Contains(result, v) {
			result = append(result, v)
		}
	}
	slices.Sort(result)
	return result
}

func union(own, pinned []string) []string {
	result := slices.Clone(own)
	for _, state := range pinned {
		if !slices.Contains(result, state) {
			result = append(result, state)
		}
	}
	return result
}

func collapse(text string) string { return strings.Join(strings.Fields(text), " ") }
