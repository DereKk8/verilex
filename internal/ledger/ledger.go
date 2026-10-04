// Package ledger owns the record of green word results and the decision to reuse them.
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/verdict"
)

const version = 1

// Entry is one green word result and the stamp it was proven under.
type Entry struct {
	Label       string            `json:"label"`
	Verdict     verdict.Verdict   `json:"verdict"`
	Stamp       string            `json:"stamp"`
	Components  map[string]string `json:"components"`
	Run         string            `json:"run"`
	Evidence    string            `json:"evidence"`
	Observation any               `json:"observation"`
	Recorded    string            `json:"recorded"`
}

type document struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

// Ledger is a project's file of green results, keyed by stamp slot.
type Ledger struct{ path string }

// At opens the ledger kept in dir.
func At(dir string) Ledger { return Ledger{filepath.Join(dir, "ledger.json")} }

// Reuse returns a recorded green entry for every step when each step's stamp matches one exactly
// and its evidence still exists. Otherwise it returns why the chain has to run.
// Reuse is all or nothing: every run launches a fresh instance, so a step that runs needs the
// effects of every step before it, and every step after it depends on its new result.
func (l Ledger) Reuse(labels []string, stamps []stamp.Stamp) ([]Entry, string) {
	doc, err := l.read(lockShared)
	if err != nil {
		return nil, "the ledger is unreadable: " + err.Error()
	}
	entries := make([]Entry, 0, len(stamps))
	for i, s := range stamps {
		if s.Unclear != "" {
			return nil, labels[i] + ": " + s.Unclear
		}
		entry, ok := doc.Entries[s.Slot]
		if !ok || entry.Verdict != verdict.Green {
			return nil, labels[i] + ": no green result on record"
		}
		if entry.Stamp != s.Digest {
			why := stamp.Diff(entry.Components, s.Components)
			if why == "" {
				why = "stamp changed"
			}
			return nil, labels[i] + ": " + why
		}
		if info, err := os.Stat(filepath.Join(entry.Evidence, "stdout")); err != nil || info.IsDir() {
			return nil, labels[i] + ": evidence from run " + entry.Run + " is gone"
		}
		entries = append(entries, entry)
	}
	return entries, ""
}

// Record stores green entries by slot, replacing what each slot held.
func (l Ledger) Record(entries map[string]Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0700); err != nil {
		return err
	}
	unlock, err := lock(l.path+".lock", lockExclusive)
	if err != nil {
		return err
	}
	defer unlock()
	doc, err := load(l.path)
	if err != nil {
		// An unreadable ledger proves nothing; start a fresh one rather than keep guessing.
		doc = document{Version: version, Entries: map[string]Entry{}}
	}
	for slot, entry := range entries {
		doc.Entries[slot] = entry
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), "ledger-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), l.path)
}

func (l Ledger) read(mode int) (document, error) {
	if _, err := os.Stat(filepath.Dir(l.path)); errors.Is(err, os.ErrNotExist) {
		return document{Version: version, Entries: map[string]Entry{}}, nil
	}
	unlock, err := lock(l.path+".lock", mode)
	if err != nil {
		return document{}, err
	}
	defer unlock()
	return load(l.path)
}

func load(path string) (document, error) {
	doc := document{Version: version, Entries: map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return doc, nil
	}
	if err != nil {
		return doc, err
	}
	if err = json.Unmarshal(data, &doc); err != nil {
		return doc, err
	}
	if doc.Version != version {
		return document{Version: version, Entries: map[string]Entry{}}, errors.New("unknown ledger version")
	}
	if doc.Entries == nil {
		doc.Entries = map[string]Entry{}
	}
	return doc, nil
}
