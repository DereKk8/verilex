// Package ledger owns the record of green word results and the decision to reuse them: the
// shared ledger of passes every verilex instance reads and writes (see Store), and the history
// of a kept instance.
package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/verdict"
)

// MaxAge is how long a recorded green result stands as proof; an older one is never reused.
const MaxAge = 7 * 24 * time.Hour

// Entry is one word result and the stamp it was proven under. The ledger holds only green
// entries; a kept instance's history holds every word that drove it, whatever its verdict.
type Entry struct {
	Label      string            `json:"label"`
	Verdict    verdict.Verdict   `json:"verdict"`
	Stamp      string            `json:"stamp"`
	Components map[string]string `json:"components"`
	// Claim is the claim version the word proved, <claim>@<version>; empty for a word without one.
	Claim string `json:"claim,omitempty"`
	// Run recorded the result; Owner is the run that launched the instance it drove.
	Run         string `json:"run"`
	Owner       string `json:"owner,omitempty"`
	Evidence    string `json:"evidence"`
	Observation any    `json:"observation"`
	Recorded    string `json:"recorded"`
	// Slot and ReadOnly are kept only in an instance's history: where in its chain the word
	// ran, and whether its contract declared that it never changes the instance.
	Slot     string `json:"slot,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
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
	if why := fresh(entry.Run, entry.Recorded, now); why != "" {
		return why
	}
	if s.Hold != "" {
		return s.Hold
	}
	return ""
}

// fresh says why a result recorded by run at recorded no longer stands: it has no readable date,
// is dated in the future, or is MaxAge old or older. It is "" while the result stands.
func fresh(run, recorded string, now time.Time) string {
	at, err := time.Parse(time.RFC3339, recorded)
	switch {
	case err != nil:
		return "the result from run " + run + " has no readable date"
	case at.After(now.Add(time.Minute)):
		return "the result from run " + run + " is dated in the future"
	case now.Sub(at) >= MaxAge:
		return fmt.Sprintf("the green result from run %s expired (%dd old; results stand 7d)", run, int(now.Sub(at).Hours()/24))
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
