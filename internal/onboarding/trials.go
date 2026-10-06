package onboarding

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/verdict"
)

// Healthy names the trial state without any planted defect.
const Healthy = "healthy"

// notRun is a trial's verdict when an earlier step stopped the chain before the word ran.
const notRun = "not run"

// parallel bounds how many trials run at once; each launches its own instance.
const parallel = 4

// Trial is one word driven on one state.
type Trial struct {
	// State is Healthy or a planted defect, and Expect the verdict the correctness gate requires
	// of the word there (empty where the gate requires nothing).
	State  string `json:"state"`
	Expect string `json:"expect,omitempty"`
	Word   string `json:"word"`
	Chain  string `json:"chain"`
	// Verdict is green, red, inconclusive, or "not run" when an earlier step stopped the chain.
	Verdict     string `json:"verdict"`
	Reason      string `json:"reason,omitempty"`
	Observation any    `json:"observation,omitempty"`
	// Run is the trial's run directory, Evidence the word step's evidence directory.
	Run      string `json:"run"`
	Evidence string `json:"evidence,omitempty"`

	env   runner.Env
	steps []dictionary.Step
}

// state is a product state a trial runs on.
type state struct {
	name   string
	defect *dictionary.Defect
	// owner is the claim whose planted defect it is; empty for Healthy.
	owner string
}

// states lists Healthy and every planted defect of the given claims, in name order. Two claims
// that declare one defect under one name share the state; a name that two claims define
// differently becomes two states, each named after its claim.
func states(claims ...dictionary.Claim) []state {
	result := []state{{name: Healthy}}
	seen := map[string]dictionary.Defect{}
	clash := map[string]bool{}
	for _, c := range claims {
		for name, defect := range c.Defects {
			if other, ok := seen[name]; ok && other.Digest() != defect.Digest() {
				clash[name] = true
			}
			seen[name] = defect
		}
	}
	added := map[string]bool{}
	for _, c := range claims {
		for _, name := range slices.Sorted(maps.Keys(c.Defects)) {
			label := name
			if clash[name] {
				label = c.Name + ":" + name
			}
			if !added[label] {
				added[label] = true
				defect := c.Defects[name]
				result = append(result, state{name: label, defect: &defect, owner: c.Name})
			}
		}
	}
	slices.SortStableFunc(result[1:], func(a, b state) int { return strings.Compare(a.name, b.name) })
	return result
}

// env plants a state: the healthy product unsets every variable any of the states' defects
// sets; a defect sets its own and unsets the rest.
func (s state) env(all []state) runner.Env {
	env := runner.Env{Set: map[string]string{}}
	if s.defect != nil {
		maps.Copy(env.Set, s.defect.Env)
	}
	for _, other := range all {
		if other.defect == nil {
			continue
		}
		for key := range other.defect.Env {
			if _, set := env.Set[key]; !set && !slices.Contains(env.Unset, key) {
				env.Unset = append(env.Unset, key)
			}
		}
	}
	slices.Sort(env.Unset)
	return env
}

// trials runs every trial, up to parallel at once, each in its own directory under dir.
func trials(p dictionary.Project, dir string, specs []Trial, first int) []Trial {
	results := make([]Trial, len(specs))
	slots := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, spec := range specs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i] = trial(p, filepath.Join(dir, fmt.Sprintf("%02d-%s-%s", first+i+1, spec.State, spec.Word)), spec)
		}()
	}
	wg.Wait()
	return results
}

// trial runs one chain on one state and reads the verdict of its last step, the word on trial.
func trial(p dictionary.Project, dir string, t Trial) Trial {
	t.Run = dir
	record, err := runner.Trial(p, t.Chain, t.steps, dir, t.env)
	if err != nil {
		t.Verdict, t.Reason = string(verdict.Inconclusive), err.Error()
		return t
	}
	last := len(t.steps) - 1
	inconclusive := record.Verdict == nil || *record.Verdict == verdict.Inconclusive
	why := ""
	if record.Reason != nil {
		why = *record.Reason
	}
	switch {
	case inconclusive:
		t.Verdict, t.Reason = string(verdict.Inconclusive), why
	case len(record.Words) <= last:
		t.Verdict, t.Reason = notRun, why
	default:
		word := record.Words[last]
		t.Verdict, t.Observation = string(word.Verdict), word.Observation
		if dictionary.Truthy(word.Reason) {
			t.Reason = fmt.Sprint(word.Reason)
		}
	}
	if len(record.Words) > last {
		t.Evidence = record.Words[last].Evidence
	}
	return t
}

// sameStates builds the chain a word is tried on: the chain of its latest counted use that still
// parses, up to and including its step there. The states the word was really used in are the
// states every variant is compared on.
func sameStates(uses []string, args [][]string, word string, words []dictionary.Word) (string, int, error) {
	for i := len(uses) - 1; i >= 0; i-- {
		segments := strings.Split(uses[i], "|")
		for k, segment := range segments {
			steps, err := dictionary.ParseChain(segment, words)
			if err != nil || steps[0].Word.Name != word || !slices.Equal(steps[0].Argv, args[i]) {
				continue
			}
			chain := strings.TrimSpace(strings.Join(segments[:k+1], "|"))
			if steps, err = dictionary.ParseChain(chain, words); err == nil && dictionary.CheckOrder(steps) == nil {
				return chain, k, nil
			}
			break
		}
	}
	return "", 0, fmt.Errorf("no counted use of %s has a chain that still parses up to it; use it in a chain once more", word)
}

// substitute replaces the step at index k of chain with another word, binding each of its args
// by the claim arg in the same position.
func substitute(chain string, k int, word dictionary.Word, values map[int]string, claim dictionary.Claim, words []dictionary.Word) (string, []dictionary.Step, error) {
	argv := []string{shellWord(word.Name)}
	for _, arg := range word.Args {
		i := slices.Index(claim.Args, arg)
		value, ok := values[i]
		if i < 0 || !ok {
			return "", nil, fmt.Errorf("%s takes arg %s, which its claim does not name", word.Name, arg)
		}
		argv = append(argv, shellWord(value))
	}
	segments := strings.Split(chain, "|")
	segments[k] = " " + strings.Join(argv, " ")
	if k == 0 {
		segments[k] = strings.TrimSpace(segments[k])
	}
	built := strings.Join(segments[:k+1], "|")
	steps, err := dictionary.ParseChain(built, words)
	if err == nil {
		err = dictionary.CheckOrder(steps)
	}
	return built, steps, err
}
