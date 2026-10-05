// Package ledger owns the record of green word results and the decision to reuse them.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/verdict"
)

const version = 1

// MaxAge is how long a recorded green result stands as proof; an older one is never reused.
const MaxAge = 7 * 24 * time.Hour

// Entry is one word result and the stamp it was proven under. The ledger holds only green
// entries; a kept instance's history holds every word that drove it, whatever its verdict.
type Entry struct {
	Label       string            `json:"label"`
	Verdict     verdict.Verdict   `json:"verdict"`
	Stamp       string            `json:"stamp"`
	Components  map[string]string `json:"components"`
	Run         string            `json:"run"`
	Evidence    string            `json:"evidence"`
	Observation any               `json:"observation"`
	Recorded    string            `json:"recorded"`
	// Slot and ReadOnly are kept only in an instance's history: where in its chain the word
	// ran, and whether its contract declared that it never changes the instance.
	Slot     string `json:"slot,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

type document struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

// Ledger is a project's file of green results, keyed by stamp slot.
type Ledger struct{ path string }

// At opens the ledger kept in dir.
func At(dir string) Ledger { return Ledger{filepath.Join(dir, "ledger.json")} }

// Reuse returns a recorded green entry for every step when each step's stamp matches one exactly,
// its evidence still exists, it is younger than MaxAge and no step is held (its word provisional
// or drift-suspect). Otherwise it returns why the chain has to run.
// Reuse is all or nothing: every run launches a fresh instance, so a step that runs needs the
// effects of every step before it, and every step after it depends on its new result.
func (l Ledger) Reuse(labels []string, stamps []stamp.Stamp) ([]Entry, string) {
	doc, err := l.read(lockShared)
	if err != nil {
		return nil, "the ledger is unreadable: " + err.Error()
	}
	now := time.Now()
	entries := make([]Entry, 0, len(stamps))
	for i, s := range stamps {
		if s.Unclear != "" {
			return nil, labels[i] + ": " + s.Unclear
		}
		entry, ok := doc.Entries[s.Slot]
		if !ok {
			return nil, labels[i] + ": no green result on record"
		}
		if why := Proves(entry, s, now); why != "" {
			return nil, labels[i] + ": " + why
		}
		entries = append(entries, entry)
	}
	return entries, ""
}

// Proves says why entry cannot stand for a step stamped s, or "" when it can: the entry is
// green, its stamp is exactly s, its evidence still exists, it is younger than MaxAge, and the
// step's word is not held.
func Proves(entry Entry, s stamp.Stamp, now time.Time) string {
	switch {
	case s.Unclear != "":
		return s.Unclear
	case entry.Verdict != verdict.Green:
		return "no green result on record"
	case entry.Stamp != s.Digest:
		if why := stamp.Diff(entry.Components, s.Components); why != "" {
			return why
		}
		return "stamp changed"
	}
	if info, err := os.Stat(filepath.Join(entry.Evidence, "stdout")); err != nil || info.IsDir() {
		return "evidence from run " + entry.Run + " is gone"
	}
	recorded, err := time.Parse(time.RFC3339, entry.Recorded)
	switch {
	case err != nil:
		return "the result from run " + entry.Run + " has no readable date"
	case recorded.After(now.Add(time.Minute)):
		return "the result from run " + entry.Run + " is dated in the future"
	case now.Sub(recorded) >= MaxAge:
		return fmt.Sprintf("the green result from run %s expired (%dd old; results stand 7d)", entry.Run, int(now.Sub(recorded).Hours()/24))
	}
	if s.Hold != "" {
		return s.Hold
	}
	return ""
}

// Continue decides which steps a kept instance has already proven, given history, every word
// that has driven the instance in order. It skips the longest prefix of the chain whose steps
// each match the instance's next history entry and that entry proves (see Proves); every step
// after it runs live on the instance, and rerun says why the first of them must.
//
// A word that changes state never runs twice on one instance, and never runs on top of effects
// the chain before it would not have built: when the first live step would follow history that
// still holds a word that is not read-only, Continue refuses. Read-only history entries changed
// nothing, so they are passed over.
func Continue(history []Entry, labels []string, stamps []stamp.Stamp) (reused []Entry, rerun string, err error) {
	now := time.Now()
	j := 0
	for i, s := range stamps {
		match, why := -1, ""
		for k := j; k < len(history) && match < 0; k++ {
			if history[k].Slot == s.Slot {
				if why = Proves(history[k], s, now); why == "" {
					match = k
				}
			}
			if !history[k].ReadOnly {
				break
			}
		}
		if match >= 0 {
			reused = append(reused, history[match])
			j = match + 1
			continue
		}
		rerun = labels[i] + ": not run on the kept instance yet"
		if why != "" {
			rerun = labels[i] + ": " + why
		} else {
			for _, h := range history[j:] {
				if !h.ReadOnly {
					rerun = labels[i] + ": the kept instance ran " + h.Label + " here"
					break
				}
			}
		}
		for _, held := range history[j:] {
			if !held.ReadOnly {
				return nil, "", fmt.Errorf("%s; the kept instance already holds the effects of %s from run %s, and a word that changes state never runs twice or out of order on one instance: run without --continue", rerun, held.Label, held.Run)
			}
		}
		return reused, rerun, nil
	}
	return reused, "", nil
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
