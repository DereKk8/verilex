// Package runner owns the trust frame, the run record, and when a chain runs live or is skipped.
package runner

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/ledger"
	"github.com/DereKk8/verilex/internal/stamp"
	"github.com/DereKk8/verilex/internal/verdict"
	"github.com/dlclark/regexp2"
)

const frameTimeout = 1800

type Run struct {
	project  dictionary.Project
	steps    []dictionary.Step
	patterns []*regexp2.Regexp
	record   Record
	// keep is set when the instance outlives the run, which is when its history is recorded.
	keep bool
	// release lets go of the lock the run holds for as long as it lives (see Claim).
	release func()
}

// New records a run under an id no other run of the project holds and keeps hold of it until
// Execute returns: while it lives, no other verilex process may tear down or take over its instance.
func New(project dictionary.Project, chain string, steps []dictionary.Step) (*Run, error) {
	patterns, err := patterns(project)
	if err != nil {
		return nil, err
	}
	id, dir, err := reserve(RunsDir(project))
	if err != nil {
		return nil, err
	}
	release, err := tryLock(filepath.Join(dir, "run.lock"))
	if err != nil {
		return nil, err
	}
	r := &Run{project: project, steps: steps, patterns: patterns, release: release, record: Record{
		Run: id, Project: project.Name, Root: project.Root, Chain: chain, Steps: len(steps), Dir: dir, Started: now(), Frame: []Frame{}, Words: []WordRecord{}, Cleanup: "pending",
	}}
	if err := r.save(); err != nil {
		release()
		return nil, err
	}
	return r, nil
}

// reserve creates the directory of a new run under an id no other run of the project holds. The
// id labels everything the run launches, so two runs never share one; its random part keeps
// runs recorded under different state homes apart too.
func reserve(runs string) (id, dir string, err error) {
	if err = os.MkdirAll(runs, 0700); err != nil {
		return "", "", err
	}
	for {
		var suffix [6]byte
		if _, err = rand.Read(suffix[:]); err != nil {
			return "", "", err
		}
		id = fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(suffix[:]))
		dir = filepath.Join(runs, id)
		if err = os.Mkdir(dir, 0700); !errors.Is(err, fs.ErrExist) {
			return id, dir, err
		}
	}
}

// Refused stops a run before it starts anything: the run leaves no record behind.
type Refused struct{ Err error }

func (e Refused) Error() string { return e.Err.Error() }
func (e Refused) Unwrap() error { return e.Err }

// Execute carries out a plan made by Decide for this chain. A fully skipped plan on a fresh
// instance launches nothing; otherwise the chain runs inside the trust frame, on a fresh instance
// or on the kept one the plan continues, and the steps the plan skips reuse their proof.
func (r *Run) Execute(p Plan, opts Options) (Record, error) {
	defer r.release()
	r.record.Rerun, r.record.Ticket, r.keep = p.Rerun, opts.Ticket, opts.Keep
	if p.kept == nil && p.Skipped() {
		return r.reuse(p.entries, p.stamps)
	}
	if p.kept != nil {
		if err := r.handOver(p.kept); err != nil {
			// Nothing ran and nothing was launched, so the run leaves no record.
			os.RemoveAll(r.record.Dir)
			return Record{}, Refused{err}
		}
	}
	base := len(r.record.History)
	err := r.drive(p)
	r.settle(p.stamps, base)
	if err != nil {
		r.stop(verdict.Inconclusive, err.Error())
	}
	if opts.Keep {
		r.record.Cleanup = "kept"
	} else if r.record.Instance != nil || p.kept == nil {
		if cleanErr := Cleanup(r.project, &r.record, r.record.Dir); cleanErr != nil {
			r.record.Verdict = ptr(verdict.Inconclusive)
			r.record.Reason = ptr(cleanErr.Error())
			err = cleanErr
		}
	}
	if saveErr := r.save(); saveErr != nil {
		return r.record, saveErr
	}
	// A ledger that misses a result only costs a later re-run, so a failed write never changes the verdict.
	_ = ledger.At(RunsDir(r.project)).Record(r.proven(p.stamps))
	return r.record, err
}

// handOver makes this run the owner of a kept instance: from now on only this run may drive or
// tear it down. Claim and the second read under it let exactly one run take over an instance.
func (r *Run) handOver(kept *Record) error {
	current, release, err := Claim(r.project, kept.Run)
	if err != nil {
		return err
	}
	defer release()
	switch {
	case current.Cleanup == "continued":
		return fmt.Errorf("%s was continued by %s; continue that run instead", current.Run, current.ContinuedBy)
	case current.Cleanup != "kept":
		return fmt.Errorf("%s no longer keeps its instance (cleanup=%s)", current.Run, current.Cleanup)
	}
	r.record.Instance, r.record.Owner, r.record.Continues = current.Instance, current.owner(), current.Run
	if r.keep {
		r.record.History = append([]ledger.Entry{}, current.History...)
	}
	if err = r.save(); err != nil {
		return err
	}
	current.Cleanup, current.ContinuedBy = "continued", r.record.Run
	return Save(current.Dir, &current)
}

// reuse records a run that relies on earlier green results instead of launching anything.
func (r *Run) reuse(entries []ledger.Entry, stamps []stamp.Stamp) (Record, error) {
	for i := range r.steps {
		r.record.Words = append(r.record.Words, r.reused(i, entries[i], stamps[i]))
	}
	r.record.Skipped = true
	r.record.Verdict = ptr(verdict.Green)
	r.record.Cleanup = "none"
	r.record.EvidenceKept = ptr(true)
	r.record.Finished = now()
	return r.record, r.save()
}

func (r *Run) reused(i int, entry ledger.Entry, s stamp.Stamp) WordRecord {
	step := r.steps[i]
	return WordRecord{
		Word: step.Word.Name, Args: step.Argv, Provides: step.Provides, Implements: step.Word.Implements, Proves: step.Word.Proves(), Entry: step.Word.Entry,
		Verdict: verdict.Green, Observation: entry.Observation, Evidence: entry.Evidence,
		Stamp: s.Digest, ReliesOn: entry.Run,
	}
}

// proven returns the ledger entries for this run's green steps. A run that ended inconclusive
// proves nothing, and a step whose stamp changed while the run was going is not recorded. A run
// on a continued instance proves nothing for a fresh one: that instance's history is not what
// the stamps claim, so its results stand only in the instance's own history.
func (r *Run) proven(before []stamp.Stamp) map[string]ledger.Entry {
	entries := map[string]ledger.Entry{}
	if r.record.Verdict == nil || *r.record.Verdict == verdict.Inconclusive || r.record.Continues != "" {
		return entries
	}
	after := stamp.Chain(r.project, r.steps)
	for i, word := range r.record.Words {
		if word.Verdict != verdict.Green || word.ReliesOn != "" || before[i].Digest == "" || before[i].Digest != after[i].Digest {
			continue
		}
		entries[before[i].Slot] = r.entry(i, word, before[i])
	}
	return entries
}

// settle drops the proof of every history entry this run added whose stamp changed while the
// run was going, for example because refresh rewrote an input: the instance holds its effects,
// but nothing may rely on them, so a later --continue that needs the word again refuses.
func (r *Run) settle(before []stamp.Stamp, base int) {
	if len(r.record.History) == base {
		return
	}
	after := stamp.Chain(r.project, r.steps)
	for k := base; k < len(r.record.History); k++ {
		for i, s := range before {
			if s.Slot == r.record.History[k].Slot && s.Digest != after[i].Digest {
				r.record.History[k].Stamp = ""
			}
		}
	}
}

func (r *Run) entry(i int, word WordRecord, s stamp.Stamp) ledger.Entry {
	return ledger.Entry{
		Label: r.steps[i].Label(), Verdict: word.Verdict, Stamp: s.Digest, Components: s.Components,
		Run: r.record.Run, Evidence: word.Evidence, Observation: word.Observation, Recorded: now(),
	}
}

func (r *Run) drive(p Plan) error {
	if p.kept != nil {
		return r.resume(p)
	}
	frame, err := r.frame("launch", "launch")
	if err != nil {
		return err
	}
	if frame.Exit == nil || *frame.Exit != 0 {
		r.stop(verdict.Inconclusive, "launch exited "+exitText(frame.Exit))
		return nil
	}
	data, err := os.ReadFile(filepath.Join(frame.Evidence, "stdout"))
	if err != nil {
		return err
	}
	var launched map[string]any
	if json.Unmarshal(data, &launched) != nil {
		r.stop(verdict.Inconclusive, "launch printed no JSON object with an 'instance'")
		return nil
	}
	instance, ok := launched["instance"]
	if !ok {
		r.stop(verdict.Inconclusive, "launch printed no JSON object with an 'instance'")
		return nil
	}
	r.record.Instance = instance
	if r.keep {
		r.record.History = []ledger.Entry{}
	}
	if err = r.save(); err != nil {
		return err
	}
	return r.words(p)
}

// resume refreshes a kept instance up to the current checkout, keeping its states, before the
// doctor vouches for it again.
func (r *Run) resume(p Plan) error {
	frame, err := r.frame("refresh", "refresh")
	if err != nil {
		return err
	}
	if frame.Exit == nil || *frame.Exit != 0 {
		r.stop(verdict.Inconclusive, "refresh exited "+exitText(frame.Exit))
		return nil
	}
	return r.words(p)
}

// words runs the chain on the instance, after the doctor vouches for it. The plan's skipped
// steps come first: they reuse proof the instance or the ledger already holds.
func (r *Run) words(p Plan) error {
	stamps := p.stamps
	healthy, err := r.doctor("doctor")
	if err != nil || !healthy {
		return err
	}
	available := map[string]bool{}
	for index, step := range r.steps {
		if index < len(p.entries) {
			r.record.Words = append(r.record.Words, r.reused(index, p.entries[index], stamps[index]))
			for _, state := range step.Provides {
				available[state] = true
			}
			continue
		}
		states := make([]string, 0, len(available))
		for s := range available {
			states = append(states, s)
		}
		sort.Strings(states)
		entry, err := r.word(index+1, step, states)
		if err != nil {
			return err
		}
		entry.Stamp = stamps[index].Digest
		r.record.Words = append(r.record.Words, entry)
		if r.record.History != nil {
			applied := r.entry(index, entry, stamps[index])
			applied.Slot, applied.ReadOnly = stamps[index].Slot, step.Word.ReadOnly
			r.record.History = append(r.record.History, applied)
		}
		if err = r.save(); err != nil {
			return err
		}
		if entry.Verdict != verdict.Green {
			reason := step.Label()
			if dictionary.Truthy(entry.Reason) {
				reason += ": " + fmt.Sprint(entry.Reason)
			}
			r.stop(entry.Verdict, reason)
			_, err = r.doctor("doctor-after-failure")
			return err
		}
		for _, state := range step.Provides {
			available[state] = true
		}
	}
	r.record.Verdict = ptr(verdict.Green)
	return nil
}

func (r *Run) word(index int, step dictionary.Step, states []string) (WordRecord, error) {
	evidence := filepath.Join(r.record.Dir, fmt.Sprintf("%02d-%s", index, step.Word.Name))
	args := map[string]string{}
	for i, arg := range step.Word.Args {
		args[arg] = step.Argv[i]
	}
	state := struct {
		Run      string            `json:"run"`
		Instance any               `json:"instance"`
		States   []string          `json:"states"`
		Args     map[string]string `json:"args"`
		Evidence string            `json:"evidence"`
	}{r.record.owner(), r.record.Instance, states, args, evidence}
	stdin, err := json.Marshal(state)
	if err != nil {
		return WordRecord{}, err
	}
	started := time.Now()
	code, err := execute(append([]string{step.Word.Run()}, step.Argv...), evidence, environment(r.project, &r.record, evidence), step.Word.Timeout, string(stdin))
	if err != nil {
		return WordRecord{}, err
	}
	var stdout []byte
	if code != nil {
		if stdout, err = os.ReadFile(filepath.Join(evidence, "stdout")); err != nil {
			return WordRecord{}, err
		}
	}
	judged := verdict.Judge(code, stdout, step.Word.Timeout)
	leak, err := scan(evidence, r.patterns)
	if err != nil {
		return WordRecord{}, err
	}
	if leak != "" {
		judged.Verdict = verdict.Inconclusive
		judged.Reason = "secret pattern in evidence " + leak
	}
	return WordRecord{Word: step.Word.Name, Args: step.Argv, Provides: step.Provides, Implements: step.Word.Implements, Proves: step.Word.Proves(), Entry: step.Word.Entry, Verdict: judged.Verdict, Reported: judged.Reported, Reason: judged.Reason, Observation: judged.Observation, Detail: judged.Detail, Exit: code, Seconds: math.RoundToEven(time.Since(started).Seconds()*100) / 100, Evidence: evidence}, nil
}

func (r *Run) frame(step, label string) (Frame, error) {
	evidence := filepath.Join(r.record.Dir, "frame-"+label)
	code, err := execute([]string{r.project.Frame(step)}, evidence, environment(r.project, &r.record, evidence), frameTimeout, "")
	if err != nil {
		return Frame{}, err
	}
	f := Frame{Step: label, Exit: code, Evidence: evidence}
	f.Leak, err = scan(evidence, r.patterns)
	if err != nil {
		return f, err
	}
	if f.Leak != "" && f.Exit != nil && *f.Exit == 0 {
		f.Exit = ptr(2)
	}
	r.record.Frame = append(r.record.Frame, f)
	return f, r.save()
}

func (r *Run) doctor(label string) (bool, error) {
	f, err := r.frame("doctor", label)
	if err != nil {
		return false, err
	}
	if f.Exit == nil || *f.Exit != 0 {
		r.stop(verdict.Inconclusive, label+" refused the instance (exit "+exitText(f.Exit)+")")
		return false, nil
	}
	return true, nil
}

func (r *Run) stop(v verdict.Verdict, reason string) {
	if verdict.Overrides(r.record.Verdict, v) {
		r.record.Verdict = ptr(v)
		r.record.Reason = ptr(reason)
	}
}

func (r *Run) save() error { return Save(r.record.Dir, &r.record) }

func Cleanup(project dictionary.Project, record *Record, dir string) error {
	evidence := filepath.Join(dir, "frame-cleanup")
	code, err := execute([]string{project.Frame("cleanup")}, evidence, environment(project, record, evidence), frameTimeout, "")
	if err != nil {
		return err
	}
	f := Frame{Step: "cleanup", Exit: code, Evidence: evidence}
	p, err := patterns(project)
	if err != nil {
		return err
	}
	f.Leak, err = scan(evidence, p)
	if err != nil {
		return err
	}
	if f.Leak != "" && code != nil && *code == 0 {
		f.Exit = ptr(2)
	}
	record.Frame = append(record.Frame, f)
	record.Cleanup = "failed"
	if code != nil && *code == 0 {
		record.Cleanup = "done"
	}
	kept := true
	for _, word := range live(record.Words) {
		info, err := os.Stat(filepath.Join(word.Evidence, "stdout"))
		if err != nil || info.IsDir() {
			kept = false
		}
	}
	record.EvidenceKept = &kept
	if (code == nil || *code != 0) && (record.Verdict == nil || *record.Verdict == verdict.Green) {
		record.Verdict = ptr(verdict.Inconclusive)
		record.Reason = ptr("cleanup exited " + exitText(code) + "; the instance may outlive the run")
	}
	if !kept {
		record.Verdict = ptr(verdict.Inconclusive)
		record.Reason = ptr("evidence did not survive cleanup")
	}
	if f.Leak != "" {
		record.Verdict = ptr(verdict.Inconclusive)
		record.Reason = ptr("secret pattern in evidence " + f.Leak)
	}
	record.Finished = now()
	return Save(dir, record)
}

func environment(project dictionary.Project, record *Record, evidence string) []string {
	instance, _ := json.Marshal(record.Instance)
	values := map[string]string{"VERILEX_RUN": record.owner(), "VERILEX_PROJECT_ROOT": project.Root, "VERILEX_INSTANCE": string(instance), "VERILEX_EVIDENCE": evidence}
	env := []string{}
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		if _, ok := values[key]; !ok {
			env = append(env, v)
		}
	}
	for _, key := range []string{"VERILEX_RUN", "VERILEX_PROJECT_ROOT", "VERILEX_INSTANCE", "VERILEX_EVIDENCE"} {
		env = append(env, key+"="+values[key])
	}
	return env
}

// owner is the run that launched a record's instance; frame steps and words see it as VERILEX_RUN.
func (r Record) owner() string {
	if r.Owner != "" {
		return r.Owner
	}
	return r.Run
}

func exitText(code *int) string {
	if code == nil {
		return "None"
	}
	return fmt.Sprint(*code)
}
