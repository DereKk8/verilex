// Package usage records run data for improvement, never proof for skipping.
package usage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/verdict"
)

type Tokens struct {
	Count     int64 `json:"count"`
	Estimated bool  `json:"estimated"`
}

type Miss struct {
	Review   string `json:"review"`
	Recorded string `json:"recorded"`
}

// Entry keeps the judged record even after the run's evidence is removed.
// Nil Seconds or Verdict means the caller did not finish recording the run.
type Entry struct {
	Record     runner.Record `json:"record"`
	Change     string        `json:"change,omitempty"`
	Revision   string        `json:"revision,omitempty"`
	DiffSHA256 string        `json:"diff_sha256,omitempty"`
	Seconds    *float64      `json:"seconds"`
	Tokens     *Tokens       `json:"tokens"`
	Miss       *Miss         `json:"miss,omitempty"`
}

type Totals struct {
	Runs            int     `json:"runs"`
	Green           int     `json:"green"`
	Red             int     `json:"red"`
	Inconclusive    int     `json:"inconclusive"`
	Unfinished      int     `json:"unfinished"`
	Misses          int     `json:"misses"`
	Seconds         float64 `json:"seconds"`
	Tokens          int64   `json:"tokens"`
	TokensSaturated bool    `json:"tokens_saturated"`
	TokensUnknown   int     `json:"tokens_unknown"`
	TokensEstimated int     `json:"tokens_estimated"`
}

type Report struct {
	Totals  Totals  `json:"totals"`
	Entries []Entry `json:"entries"`
}

type Store struct{ Dir string }

func ID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(b[:]), nil
}

func validID(id string) error {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return errors.New("invalid usage run id")
	}
	return nil
}

// Save has one writer per run. Annotations use separate immutable files so a
// final snapshot cannot overwrite token costs or reviews recorded concurrently.
func (s Store) Save(entry Entry) error {
	if err := validID(entry.Record.Run); err != nil {
		return err
	}
	return publish(filepath.Join(s.Dir, entry.Record.Run, "run.json"), entry)
}

// Remove drops a start snapshot when execution refuses the run before anything runs.
func (s Store) Remove(id string) error {
	if err := validID(id); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(s.Dir, id))
}

func (s Store) read(id string) (Entry, error) {
	var entry Entry
	if err := validID(id); err != nil {
		return entry, err
	}
	err := readJSON(filepath.Join(s.Dir, id, "run.json"), &entry)
	if err == nil && entry.Record.Run != id {
		err = errors.New("usage run id does not match its directory")
	}
	return entry, err
}

type annotation struct {
	Tokens *Tokens `json:"tokens,omitempty"`
	Miss   *Miss   `json:"miss,omitempty"`
}

func (s Store) Annotate(id string, tokens *Tokens, review string) error {
	entry, err := s.read(id)
	if err != nil {
		return err
	}
	if tokens != nil && tokens.Count < 0 {
		return errors.New("token count must be nonnegative")
	}
	if tokens == nil && strings.TrimSpace(review) == "" {
		return errors.New("pass --tokens or a nonempty --miss review")
	}
	note := annotation{Tokens: tokens}
	if strings.TrimSpace(review) != "" {
		if entry.Record.Verdict == nil || *entry.Record.Verdict != verdict.Green {
			return errors.New("only a green run can be marked as a Verilex miss")
		}
		note.Miss = &Miss{Review: review, Recorded: time.Now().UTC().Format(time.RFC3339Nano)}
	}
	name, err := ID()
	if err != nil {
		return err
	}
	return publish(filepath.Join(s.Dir, id, "annotations", name+".json"), note)
}

func (s Store) Read() (Report, error) {
	result := Report{Entries: []Entry{}}
	dirs, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entry, err := s.read(dir.Name())
		if errors.Is(err, os.ErrNotExist) {
			// A writer may have made its directory but not yet published its start.
			continue
		}
		if err != nil {
			return result, err
		}
		notes, err := filepath.Glob(filepath.Join(s.Dir, dir.Name(), "annotations", "*.json"))
		if err != nil {
			return result, err
		}
		for _, path := range notes {
			var note annotation
			if err = readJSON(path, &note); err != nil {
				return result, err
			}
			if note.Tokens != nil {
				entry.Tokens = note.Tokens
			}
			if note.Miss != nil {
				entry.Miss = note.Miss
			}
		}
		if entry.Seconds != nil && (*entry.Seconds < 0 || math.IsInf(*entry.Seconds, 0) || math.IsNaN(*entry.Seconds)) {
			return result, fmt.Errorf("invalid runtime for usage run %s", dir.Name())
		}
		if entry.Miss != nil && (entry.Record.Verdict == nil || *entry.Record.Verdict != verdict.Green || strings.TrimSpace(entry.Miss.Review) == "") {
			return result, fmt.Errorf("invalid miss for usage run %s", dir.Name())
		}
		totals := &result.Totals
		totals.Runs++
		switch {
		case entry.Seconds == nil || entry.Record.Verdict == nil:
			totals.Unfinished++
		case *entry.Record.Verdict == verdict.Green:
			totals.Green++
		case *entry.Record.Verdict == verdict.Red:
			totals.Red++
		case *entry.Record.Verdict == verdict.Inconclusive:
			totals.Inconclusive++
		default:
			return result, fmt.Errorf("invalid verdict for usage run %s", dir.Name())
		}
		if entry.Miss != nil {
			totals.Misses++
		}
		if entry.Seconds != nil {
			totals.Seconds += *entry.Seconds
		}
		if entry.Tokens == nil {
			totals.TokensUnknown++
		} else {
			if entry.Tokens.Count < 0 {
				return result, fmt.Errorf("invalid token count for usage run %s", dir.Name())
			}
			if entry.Tokens.Count > math.MaxInt64-totals.Tokens {
				totals.Tokens, totals.TokensSaturated = math.MaxInt64, true
			} else {
				totals.Tokens += entry.Tokens.Count
			}
			if entry.Tokens.Estimated {
				totals.TokensEstimated++
			}
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func publish(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
