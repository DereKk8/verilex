package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/ticket"
	"github.com/DereKk8/verilex/internal/verdict"
)

type Frame struct {
	Step     string `json:"step"`
	Exit     *int   `json:"exit"`
	Evidence string `json:"evidence"`
	Leak     string `json:"leak,omitempty"`
}

// Uncovered is a touched claim this run did not prove, and the command that would prove it.
type Uncovered struct {
	Claim string `json:"claim"`
	Next  string `json:"next"`
}

// ClaimReport is one claim's verdict. Expected and Got are the failing link, set only when the
// claim is not green. Next is the command that retries it.
type ClaimReport struct {
	Claim    string          `json:"claim"`
	Proves   string          `json:"proves,omitempty"`
	Word     string          `json:"word"`
	Step     string          `json:"step"`
	Verdict  verdict.Verdict `json:"verdict"`
	Evidence string          `json:"evidence,omitempty"`
	Expected string          `json:"expected,omitempty"`
	Got      string          `json:"got,omitempty"`
	Next     string          `json:"next,omitempty"`
}

type WordRecord struct {
	Word       string   `json:"word"`
	Args       []string `json:"args"`
	Provides   []string `json:"provides"`
	Implements []string `json:"implements"`
	// Proves is the claim version the word proved (<claim>@<version>) and Entry the user entry
	// point it went through; both are empty for a word without a claim.
	Proves      string          `json:"proves,omitempty"`
	Entry       string          `json:"entry,omitempty"`
	Verdict     verdict.Verdict `json:"verdict"`
	Reported    string          `json:"reported,omitempty"`
	Reason      any             `json:"reason"`
	Observation any             `json:"observation"`
	Detail      any             `json:"detail"`
	Exit        *int            `json:"exit"`
	Seconds     float64         `json:"seconds"`
	Evidence    string          `json:"evidence"`
	Stamp       string          `json:"stamp,omitempty"`
	ReliesOn    string          `json:"relies_on,omitempty"`
}

type Record struct {
	Run      string `json:"run"`
	Project  string `json:"project"`
	Root     string `json:"root"`
	Chain    string `json:"chain"`
	Steps    int    `json:"steps"`
	Dir      string `json:"dir"`
	Started  string `json:"started"`
	Instance any    `json:"instance"`
	// Ticket is the resolved run ticket this run was given, if any.
	Ticket *ticket.Ticket `json:"ticket,omitempty"`
	// Owner is the run that launched the instance, when this run continues another's.
	Owner string `json:"owner,omitempty"`
	// Continues names the kept run whose instance this run drives; ContinuedBy, on that kept
	// run, names the run that took the instance over.
	Continues   string `json:"continues,omitempty"`
	ContinuedBy string `json:"continued_by,omitempty"`
	// History lists, in order, every word that has driven a kept instance, across the runs that
	// continued it; `verilex run --continue` decides from it what the instance already proves.
	History []ledger.Entry   `json:"history,omitempty"`
	Frame   []Frame          `json:"frame"`
	Words   []WordRecord     `json:"words"`
	Verdict *verdict.Verdict `json:"verdict"`
	Reason  *string          `json:"reason"`
	Skipped bool             `json:"skipped,omitempty"`
	Rerun   string           `json:"rerun,omitempty"`
	// Warning, Uncovered and Claims are set when this run was given a diff or a claim plan.
	// Warning is empty when every touched claim was proved. Uncovered names touched claims this
	// run did not prove, each with the next command that would prove it. Claims is one verdict
	// per claim the chain proved or failed. All three are absent on a chain run given no diff.
	Warning      string        `json:"warning,omitempty"`
	Uncovered    []Uncovered   `json:"uncovered,omitempty"`
	Claims       []ClaimReport `json:"claims,omitempty"`
	Cleanup      string        `json:"cleanup"`
	EvidenceKept *bool         `json:"evidence_kept,omitempty"`
	Finished     string        `json:"finished,omitempty"`
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

// LedgerDir is where the project's ledger of passes lives: under VERILEX_LEDGER when it is set,
// so every instance pointed at one directory shares one ledger, and under the state home otherwise.
func LedgerDir(project dictionary.Project) string {
	if shared := os.Getenv("VERILEX_LEDGER"); shared != "" {
		if abs, err := filepath.Abs(shared); err == nil {
			shared = abs
		}
		return filepath.Join(shared, project.Name)
	}
	return filepath.Join(StateHome(), project.Name, "ledger")
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
	if err = json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	// Runs recorded before the three verdicts carry pass, fail, blocked or unverified.
	if record.Verdict != nil {
		record.Verdict = ptr(verdict.Legacy(string(*record.Verdict)))
	}
	for i := range record.Words {
		record.Words[i].Verdict = verdict.Legacy(string(record.Words[i].Verdict))
	}
	return record, nil
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

// now has a fixed-width fraction, so records started within one second still sort in time order.
func now() string       { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000+00:00") }
func ptr[T any](v T) *T { return &v }
