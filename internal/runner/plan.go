package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/ticket"
)

// Options shape one run.
type Options struct {
	// Keep leaves the instance running. A fresh run with Keep always runs live, because the kept
	// instance must hold every state the chain provides.
	Keep bool
	// Fresh runs the chain live even when every stamp matches.
	Fresh bool
	// Continue names a kept run whose instance this run drives instead of launching its own.
	Continue string
	// Ticket is the run's resolved ticket, carried into the plan and the run record. It never
	// changes what runs or what is skipped.
	Ticket *ticket.Ticket
}

// Plan is the skip decision for one chain: `verilex plan` prints it and `verilex run` carries it
// out, so the two can never disagree.
type Plan struct {
	// Continues is the kept run whose instance the chain drives; empty for a fresh instance.
	Continues string `json:"continues,omitempty"`
	// Skip holds one decision per step.
	Skip []Skip `json:"steps"`
	// Rerun is the first reason a step runs live; empty when every step is skipped.
	Rerun string `json:"rerun,omitempty"`
	// Ticket is the resolved run ticket, when one was given.
	Ticket *ticket.Ticket `json:"ticket,omitempty"`

	stamps  []stamp.Stamp
	entries []ledger.Entry
	kept    *Record
}

// Skip is one step's decision; ReliesOn names the run whose result it reuses.
type Skip struct {
	Step     string `json:"step"`
	Skipped  bool   `json:"skipped"`
	ReliesOn string `json:"relies_on,omitempty"`
}

// Skipped reports whether every step is skipped.
func (p Plan) Skipped() bool { return p.Rerun == "" }

// Decide stamps the chain and applies the one skip rule. On a fresh instance it is all or
// nothing (ledger.Reuse); on a kept instance it skips the prefix that instance already proves
// (ledger.Continue). It runs nothing. An error refuses the chain before anything starts.
func Decide(project dictionary.Project, steps []dictionary.Step, opts Options) (Plan, error) {
	p := Plan{Continues: opts.Continue, Ticket: opts.Ticket, stamps: stamp.Chain(project, steps)}
	labels, claims := make([]string, len(steps)), make([]string, len(steps))
	for i, step := range steps {
		labels[i], claims[i] = step.Label(), step.Word.Proves()
	}
	var err error
	switch {
	case opts.Continue != "" && opts.Fresh:
		return p, errors.New("--fresh would run every word again on the kept instance; run without --continue")
	case opts.Continue != "":
		if p.kept, err = Kept(project, opts.Continue); err != nil {
			return p, err
		}
		if p.entries, p.Rerun, err = ledger.Continue(p.kept.History, labels, p.stamps); err != nil {
			return p, err
		}
	case opts.Keep:
		p.Rerun = "--keep needs a live instance"
	case opts.Fresh:
		p.Rerun = "--fresh asked for a live run"
	default:
		if p.entries, p.Rerun = ledger.At(LedgerDir(project)).Reuse(labels, claims, p.stamps); p.Rerun != "" {
			p.entries = nil
		}
	}
	for i, label := range labels {
		s := Skip{Step: label}
		if i < len(p.entries) {
			s.Skipped, s.ReliesOn = true, p.entries[i].Run
		}
		p.Skip = append(p.Skip, s)
	}
	return p, nil
}

// Kept loads a run whose instance is still alive, so another run may continue it.
func Kept(project dictionary.Project, id string) (*Record, error) {
	record, release, err := Claim(project, id)
	if err != nil {
		return nil, err
	}
	release()
	switch {
	case record.Cleanup == "continued":
		return nil, fmt.Errorf("%s was continued by %s; continue that run instead", id, record.ContinuedBy)
	case record.Cleanup != "kept":
		return nil, fmt.Errorf("%s kept no instance (cleanup=%s); run with --keep first", id, record.Cleanup)
	case record.Instance == nil:
		return nil, fmt.Errorf("%s never launched an instance", id)
	case len(record.History) < len(live(record.Words)):
		return nil, fmt.Errorf("%s records no history of what drove its instance; run without --continue", id)
	case !dictionary.Executable(project.Frame("refresh")):
		return nil, fmt.Errorf("missing executable frame step %s; --continue needs it", project.Frame("refresh"))
	}
	return &record, nil
}

// Claim takes hold of a recorded run, so that this process alone may act on its instance: tear
// it down or take it over. It refuses while another verilex process holds the run: the run
// itself while it goes, or a command already tearing down or taking over its instance. A run
// whose process died holds nothing, so its instance can still be torn down. release lets go.
func Claim(project dictionary.Project, id string) (record Record, release func(), err error) {
	dir := filepath.Join(RunsDir(project), id)
	info, err := os.Stat(filepath.Join(dir, "run.json"))
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." || err != nil || info.IsDir() {
		return record, nil, fmt.Errorf("%s is not a run of %s", id, project.Name)
	}
	release, err = tryLock(filepath.Join(dir, "run.lock"))
	if errors.Is(err, errBusy) {
		if current, readErr := ReadRecord(filepath.Join(dir, "run.json")); readErr == nil && current.Cleanup == "pending" {
			return record, nil, fmt.Errorf("%s is still running and owns its instance; wait until it finishes", id)
		}
		return record, nil, fmt.Errorf("%s's instance is in use by another verilex command; try again once it finishes", id)
	}
	if err != nil {
		return record, nil, err
	}
	if record, err = ReadRecord(filepath.Join(dir, "run.json")); err != nil {
		release()
		return record, nil, err
	}
	return record, release, nil
}

// live returns the words a run drove itself, leaving out results it reused.
func live(words []WordRecord) []WordRecord {
	result := []WordRecord{}
	for _, word := range words {
		if word.ReliesOn == "" {
			result = append(result, word)
		}
	}
	return result
}
