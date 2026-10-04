// Package runner owns the trust frame and persisted evidence ledger.
package runner

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/dlclark/regexp2"
)

const frameTimeout = 1800

type Run struct {
	project  dictionary.Project
	steps    []dictionary.Step
	patterns []*regexp2.Regexp
	record   Record
}

func New(project dictionary.Project, chain string, steps []dictionary.Step) (*Run, error) {
	var suffix [3]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return nil, err
	}
	id := fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(suffix[:]))
	dir := filepath.Join(RunsDir(project), id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	patterns, err := patterns(project)
	if err != nil {
		return nil, err
	}
	r := &Run{project: project, steps: steps, patterns: patterns, record: Record{
		Run: id, Project: project.Name, Root: project.Root, Chain: chain, Dir: dir, Started: now(), Frame: []Frame{}, Words: []WordRecord{}, Cleanup: "pending",
	}}
	if err := r.save(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Run) Execute(keep bool) (Record, error) {
	err := r.drive()
	if err != nil {
		r.stop("blocked", err.Error())
	}
	if keep {
		r.record.Cleanup = "kept"
	} else {
		if cleanErr := Cleanup(r.project, &r.record, r.record.Dir); cleanErr != nil {
			r.record.Verdict = ptr("blocked")
			r.record.Reason = ptr(cleanErr.Error())
			err = cleanErr
		}
	}
	if saveErr := r.save(); saveErr != nil {
		return r.record, saveErr
	}
	return r.record, err
}

func (r *Run) drive() error {
	frame, err := r.frame("launch", "launch")
	if err != nil {
		return err
	}
	if frame.Exit == nil || *frame.Exit != 0 {
		r.stop("blocked", "launch exited "+exitText(frame.Exit))
		return nil
	}
	data, err := os.ReadFile(filepath.Join(frame.Evidence, "stdout"))
	if err != nil {
		return err
	}
	var launched map[string]any
	if json.Unmarshal(data, &launched) != nil {
		r.stop("blocked", "launch printed no JSON object with an 'instance'")
		return nil
	}
	instance, ok := launched["instance"]
	if !ok {
		r.stop("blocked", "launch printed no JSON object with an 'instance'")
		return nil
	}
	r.record.Instance = instance
	if err = r.save(); err != nil {
		return err
	}
	healthy, err := r.doctor("doctor")
	if err != nil || !healthy {
		return err
	}
	available := map[string]bool{}
	for index, step := range r.steps {
		states := make([]string, 0, len(available))
		for s := range available {
			states = append(states, s)
		}
		sort.Strings(states)
		entry, err := r.word(index+1, step, states)
		if err != nil {
			return err
		}
		r.record.Words = append(r.record.Words, entry)
		if err = r.save(); err != nil {
			return err
		}
		if entry.Verdict != "pass" {
			reason := step.Label()
			if Truthy(entry.Reason) {
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
	r.record.Verdict = ptr("pass")
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
	}{r.record.Run, r.record.Instance, states, args, evidence}
	stdin, err := json.Marshal(state)
	if err != nil {
		return WordRecord{}, err
	}
	started := time.Now()
	code, err := execute(append([]string{step.Word.Run()}, step.Argv...), evidence, environment(r.project, &r.record, evidence), step.Word.Timeout, string(stdin))
	if err != nil {
		return WordRecord{}, err
	}
	verdict, reason, result, err := judge(code, evidence, step.Word.Timeout)
	if err != nil {
		return WordRecord{}, err
	}
	leak, err := scan(evidence, r.patterns)
	if err != nil {
		return WordRecord{}, err
	}
	if leak != "" {
		verdict = "unverified"
		reason = "secret pattern in evidence " + leak
	}
	return WordRecord{Word: step.Word.Name, Args: step.Argv, Provides: step.Provides, Implements: step.Word.Implements, Verdict: verdict, Reason: reason, Observation: result.Observation, Detail: result.Detail, Exit: code, Seconds: math.RoundToEven(time.Since(started).Seconds()*100) / 100, Evidence: evidence}, nil
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
		r.stop("blocked", label+" refused the instance (exit "+exitText(f.Exit)+")")
		return false, nil
	}
	return true, nil
}

func (r *Run) stop(verdict, reason string) {
	if r.record.Verdict == nil || (*r.record.Verdict == "fail" && verdict == "blocked") {
		r.record.Verdict = ptr(verdict)
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
	for _, word := range record.Words {
		info, err := os.Stat(filepath.Join(word.Evidence, "stdout"))
		if err != nil || info.IsDir() {
			kept = false
		}
	}
	record.EvidenceKept = &kept
	if (code == nil || *code != 0) && (record.Verdict == nil || *record.Verdict == "pass") {
		record.Verdict = ptr("blocked")
		record.Reason = ptr("cleanup exited " + exitText(code) + "; the instance may outlive the run")
	}
	if !kept {
		record.Verdict = ptr("unverified")
		record.Reason = ptr("evidence did not survive cleanup")
	}
	if f.Leak != "" {
		record.Verdict = ptr("unverified")
		record.Reason = ptr("secret pattern in evidence " + f.Leak)
	}
	record.Finished = now()
	return Save(dir, record)
}

func environment(project dictionary.Project, record *Record, evidence string) []string {
	instance, _ := json.Marshal(record.Instance)
	values := map[string]string{"VERILEX_RUN": record.Run, "VERILEX_PROJECT_ROOT": project.Root, "VERILEX_INSTANCE": string(instance), "VERILEX_EVIDENCE": evidence}
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

func exitText(code *int) string {
	if code == nil {
		return "None"
	}
	return fmt.Sprint(*code)
}
