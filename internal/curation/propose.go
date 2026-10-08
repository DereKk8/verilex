package curation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/fingerprint"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/verdict"
)

// MinRuns is how many different runs must have used a word before it can be proposed.
const MinRuns = 2

// Use is one run in which a word judged the product: its step was green or red in a run that
// was itself green or red, proving the claim version the word pins now. Inconclusive steps say
// nothing about the word, an inconclusive run proves nothing (the same rule the ledger
// follows), a skipped run relied on an earlier run instead of driving the product, and a step
// that proved another claim version is evidence only for that version, so none of them count.
type Use struct {
	Run         string          `json:"run"`
	Chain       string          `json:"chain"`
	Args        []string        `json:"args"`
	Verdict     verdict.Verdict `json:"verdict"`
	Observation any             `json:"observation,omitempty"`
	Detail      any             `json:"detail,omitempty"`
	Evidence    string          `json:"evidence"`
}

// Uses lists the counted uses of a word, one per run, oldest run first.
func Uses(p dictionary.Project, w dictionary.Word) ([]Use, error) {
	records, err := runner.LoadRuns(p)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Started < records[j].Started })
	uses := []Use{}
	for _, record := range records {
		if record.Skipped || !judged(record.Verdict) {
			continue
		}
		for _, entry := range record.Words {
			if entry.Word == w.Name && entry.Proves == w.Proves() && entry.ReliesOn == "" && judged(&entry.Verdict) {
				uses = append(uses, Use{record.Run, record.Chain, entry.Args, entry.Verdict, entry.Observation, entry.Detail, entry.Evidence})
				break
			}
		}
	}
	return uses, nil
}

// judged reports whether v is a verdict on the product: green or red, never inconclusive.
func judged(v *verdict.Verdict) bool {
	return v != nil && (*v == verdict.Green || *v == verdict.Red)
}

// Entry is one word of the dictionary as the curator sees it.
type Entry struct {
	Word       string          `json:"word"`
	Status     lifecycle.State `json:"status"`
	Promise    string          `json:"promise"`
	Args       []string        `json:"args"`
	Requires   []string        `json:"requires"`
	Provides   []string        `json:"provides"`
	Implements []string        `json:"implements"`
	Proves     string          `json:"proves,omitempty"`
}

// Packet is everything a curator needs to judge one word, in one file.
type Packet struct {
	ID         string               `json:"id"`
	Project    string               `json:"project"`
	Created    string               `json:"created"`
	Word       string               `json:"word"`
	Status     lifecycle.State      `json:"status"`
	Files      map[string]string    `json:"files"`
	WordDigest string               `json:"word_digest"`
	Binding    map[string]string    `json:"binding"`
	Uses       []Use                `json:"uses"`
	Sections   []featuremap.Section `json:"sections"`
	Dictionary []Entry              `json:"dictionary"`
	Curator    string               `json:"curator_instructions"`
}

const instructions = `Judge whether this word belongs in the shared dictionary. Admit it only when its promise is one real product moment a user would recognize, its run proves exactly that promise with a second observation, its uses' evidence backs the verdicts they report, it faithfully implements the feature-map sections listed, and no dictionary word already covers it. Write your verdict as a JSON file {"word": <word>, "packet": <id>, "verdict": "admit" or "reject", "curator": <the model that judged>, "reason": <one or two sentences>} and record it with ` + "`verilex admit <word> --verdict <file>`."

// Propose builds the curator packet for a word and stores it in the project's state
// directory. It refuses a word with fewer than MinRuns counted uses and an admitted word
// whose sections (or claim version and sources) and files are unchanged.
func Propose(p dictionary.Project, name string) (Packet, string, error) {
	words, err := lifecycle.LoadWords(p)
	if err != nil {
		return Packet{}, "", err
	}
	w, err := find(words, name)
	if err != nil {
		return Packet{}, "", err
	}
	if err = onboardOnly(w); err != nil {
		return Packet{}, "", err
	}
	status, err := lifecycle.StatusOf(p, w)
	if err != nil {
		return Packet{}, "", err
	}
	if status.State == lifecycle.Admitted {
		return Packet{}, "", fmt.Errorf("%s is already admitted and nothing it implements changed", name)
	}
	uses, err := Uses(p, w)
	if err != nil {
		return Packet{}, "", err
	}
	if len(uses) < MinRuns {
		return Packet{}, "", fmt.Errorf("%s has counted uses in %d run(s); propose needs at least %d different runs in which it was green or red", name, len(uses), MinRuns)
	}
	implemented, err := sections(p, w)
	if err != nil {
		return Packet{}, "", fmt.Errorf("%v; point the word at a feature-map section, or record the moment with `verilex gap`", err)
	}
	files, err := wordFiles(w)
	if err != nil {
		return Packet{}, "", err
	}
	digest, err := lifecycle.WordDigest(w)
	if err != nil {
		return Packet{}, "", err
	}
	binding, err := fingerprint.Digests(p)
	if err != nil {
		return Packet{}, "", err
	}
	dictionaryEntries := make([]Entry, 0, len(words))
	for _, other := range words {
		s, err := lifecycle.StatusOf(p, other)
		if err != nil {
			return Packet{}, "", err
		}
		dictionaryEntries = append(dictionaryEntries, Entry{other.Name, s.State, other.Promise, other.Args, other.Requires, other.Provides, other.Implements, other.Proves()})
	}
	packet := Packet{Project: p.Name, Created: now(), Word: name, Status: status.State, Files: files, WordDigest: digest, Binding: binding, Uses: uses, Sections: implemented, Dictionary: dictionaryEntries, Curator: instructions}
	data, err := json.Marshal(packet)
	if err != nil {
		return Packet{}, "", err
	}
	sum := sha256.Sum256(data)
	packet.ID = hex.EncodeToString(sum[:8])
	dir := proposalsDir(p, name)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Packet{}, "", err
	}
	path := filepath.Join(dir, packet.ID+".json")
	return packet, path, writeJSON(path, packet)
}

func proposalsDir(p dictionary.Project, word string) string {
	return filepath.Join(runner.StateHome(), p.Name, "proposals", word)
}

func wordFiles(w dictionary.Word) (map[string]string, error) {
	files := map[string]string{}
	err := lifecycle.EachFile(w, func(rel string, data []byte) {
		if utf8.Valid(data) {
			files[rel] = string(data)
		} else {
			files[rel] = fmt.Sprintf("(binary, %d bytes)", len(data))
		}
	})
	return files, err
}
