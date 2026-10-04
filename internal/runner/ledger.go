package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
)

type Frame struct {
	Step     string `json:"step"`
	Exit     *int   `json:"exit"`
	Evidence string `json:"evidence"`
	Leak     string `json:"leak,omitempty"`
}

type WordRecord struct {
	Word        string   `json:"word"`
	Args        []string `json:"args"`
	Provides    []string `json:"provides"`
	Implements  []string `json:"implements"`
	Verdict     string   `json:"verdict"`
	Reason      any      `json:"reason"`
	Observation any      `json:"observation"`
	Detail      any      `json:"detail"`
	Exit        *int     `json:"exit"`
	Seconds     float64  `json:"seconds"`
	Evidence    string   `json:"evidence"`
}

type Record struct {
	Run          string       `json:"run"`
	Project      string       `json:"project"`
	Root         string       `json:"root"`
	Chain        string       `json:"chain"`
	Dir          string       `json:"dir"`
	Started      string       `json:"started"`
	Instance     any          `json:"instance"`
	Frame        []Frame      `json:"frame"`
	Words        []WordRecord `json:"words"`
	Verdict      *string      `json:"verdict"`
	Reason       *string      `json:"reason"`
	Cleanup      string       `json:"cleanup"`
	EvidenceKept *bool        `json:"evidence_kept,omitempty"`
	Finished     string       `json:"finished,omitempty"`
}

func StateHome() string {
	if home := os.Getenv("VERILEX_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "verilex")
}

func RunsDir(project dictionary.Project) string {
	return filepath.Join(StateHome(), project.Name, "runs")
}

func Save(dir string, record *Record) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "run.json.tmp")
	if err = os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "run.json"))
}

func ReadRecord(path string) (Record, error) {
	var record Record
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(data, &record)
	return record, err
}

func LoadRuns(project dictionary.Project) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(RunsDir(project), "*", "run.json"))
	if err != nil {
		return nil, err
	}
	records := []Record{}
	for _, path := range paths {
		r, err := ReadRecord(path)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

func now() string       { return time.Now().UTC().Format("2006-01-02T15:04:05+00:00") }
func ptr[T any](v T) *T { return &v }

func ExitCode(verdict string) int {
	switch verdict {
	case "pass":
		return 0
	case "fail":
		return 1
	default:
		return 2
	}
}
