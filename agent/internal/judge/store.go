package judge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Store keeps judgments, so an intent is judged once and not on every run. It is a directory
// that every launcher pointed at it reads and writes, on one machine or on storage several share:
//
//	<key>/<digest>.json
//
// key stamps the intent and the judge, and digest is the file's own content. A judgment is
// never rewritten: a new one is a new file, so concurrent launchers lose nothing and mix nothing.
// A reader trusts no file: a judgment is reused only when it matches its digest and its key and
// every claim text it relied on is unchanged.
type Store struct{ dir string }

// At opens the store kept in dir.
func At(dir string) Store { return Store{dir: dir} }

// DefaultDir is the store a launcher uses when it is given none:
// $XDG_STATE_HOME/verilex-agent/judgments, by default under ~/.local/state. Without either
// variable there is no default.
func DefaultDir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("no judgment store: pass --judgments or set HOME")
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "verilex-agent", "judgments"), nil
}

var judgmentName = regexp.MustCompile(`^([0-9a-f]{64})\.json$`)

// maxJudgment bounds what a reader takes from one file; a judgment is far smaller.
const maxJudgment = 1 << 20

// Find returns the stored judgment of intent by j that still holds for catalog. Of several, it
// returns the most cautious. An unreadable store finds nothing, so the intent is judged again.
func (s Store) Find(intent string, j Judge, catalog []Claim) (Judgment, bool) {
	dir := filepath.Join(s.dir, key(intent, j))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Judgment{}, false
	}
	var found []Judgment
	for _, entry := range entries {
		name := judgmentName.FindStringSubmatch(entry.Name())
		if name == nil || !entry.Type().IsRegular() {
			continue
		}
		data, err := read(filepath.Join(dir, entry.Name()))
		if err != nil || sum(data) != name[1] {
			continue
		}
		var got Judgment
		if json.Unmarshal(data, &got) != nil || got.Format != format || got.Intent != Normalize(intent) || got.Judge != j.Identity() {
			continue
		}
		if got.check(func(name string) bool { _, ok := got.Relied[name]; return ok }) != nil || !got.holds(catalog) {
			continue
		}
		got.Reused = true
		found = append(found, got)
	}
	if len(found) == 0 {
		return Judgment{}, false
	}
	slices.SortStableFunc(found, func(a, b Judgment) int {
		if a.rank() != b.rank() {
			return a.rank() - b.rank()
		}
		switch {
		case a.Confidence < b.Confidence:
			return -1
		case a.Confidence > b.Confidence:
			return 1
		}
		return strings.Compare(a.Judged, b.Judged)
	})
	return found[0], true
}

// Record stores a judgment under its key, as a new file named by its content.
func (s Store) Record(j Judgment) error {
	data, err := json.MarshalIndent(j, "", " ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Join(s.dir, keyOf(j.Intent, j.Judge))
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	// The name is the content's digest, so a writer that wins the rename wrote the same bytes.
	return os.Rename(tmp.Name(), filepath.Join(dir, sum(data)+".json"))
}

// key stamps an intent and a judge: the directory their judgments share.
func key(intent string, j Judge) string { return keyOf(Normalize(intent), j.Identity()) }

func keyOf(intent, judge string) string { return digest(format, intent, judge) }

func read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxJudgment))
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
