package onboarding

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/DereKk8/verilex/internal/dictionary"
	"github.com/DereKk8/verilex/internal/featuremap"
	"github.com/DereKk8/verilex/internal/grouping"
	"github.com/DereKk8/verilex/internal/lifecycle"
	"github.com/DereKk8/verilex/internal/terms"
)

// mapping checks the agent's step-to-claim mapping (R19): every requirement sentence a source
// maps must be what the claim's evidence contract proves. A sentence with literal values (code
// spans, numbers, file names) needs more than half of them named in the claim's sentence or
// evidence contract; a sentence without any needs a significant term in common. It says why the
// mapping looks wrong, or "" when it holds.
func mapping(c dictionary.Claim, anchors []featuremap.Anchor) string {
	contract := strings.Join(append([]string{c.Sentence, c.Evidence.Action, c.Evidence.Observation}, c.Evidence.NonProofs...), "\n")
	for _, anchor := range anchors {
		for _, sentence := range anchor.Requirements {
			literals := terms.Literals(sentence)
			if len(literals) == 0 {
				if len(terms.Shared(terms.Of(sentence), terms.Of(contract))) == 0 {
					return fmt.Sprintf("claim %s maps %s to %q, which shares no term with the claim's sentence or evidence contract: map the step that proves the claim", c.Name, anchor.Ref, sentence)
				}
				continue
			}
			named := slices.DeleteFunc(slices.Clone(literals), func(literal string) bool { return !terms.Shows(contract, literal) })
			if 2*len(named) <= len(literals) {
				return fmt.Sprintf("claim %s maps %s to %q, but its evidence contract names %d of that step's %d expected values (%s): map the step that proves the claim, or name its values in the evidence", c.Name, anchor.Ref, sentence, len(named), len(literals), quoted(literals))
			}
		}
	}
	return ""
}

// onlyNonProof returns the declared non-proof an observation shows when it shows nothing else
// the claim's observation names (R13): it names every literal value of that non-proof and none
// of the significant terms that set the claim's observation apart from it.
func onlyNonProof(observation string, nonProofs []string, claimObservation string) string {
	for _, nonProof := range nonProofs {
		literals := terms.Literals(nonProof)
		if len(literals) == 0 || slices.ContainsFunc(literals, func(literal string) bool { return !terms.Shows(observation, literal) }) {
			continue
		}
		apart := slices.DeleteFunc(terms.Of(claimObservation), func(term string) bool { return slices.Contains(terms.Of(nonProof), term) })
		if len(terms.Shared(terms.Of(observation), apart)) == 0 {
			return nonProof
		}
	}
	return ""
}

// text renders a word's observation, whatever JSON value it reported, as text.
func text(observation any) string {
	if s, ok := observation.(string); ok {
		return s
	}
	data, _ := json.Marshal(observation)
	return string(data)
}

func quoted(values []string) string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = "`" + v + "`"
	}
	return strings.Join(result, ", ")
}

// crossRepo records each claim source that lives in another repository at the commit
// onboarding checks it against (R17). The source's feature file must be committed there, so the
// commit holds exactly what onboarding read.
func crossRepo(p dictionary.Project, c dictionary.Claim, anchors []featuremap.Anchor) ([]grouping.Source, error) {
	sources := []grouping.Source{}
	for i, source := range c.Sources {
		if source.Repo == "" {
			continue
		}
		root, _ := lifecycle.SourceRoot(p, source)
		commit, err := git(root, "rev-parse", "HEAD")
		if err != nil {
			return nil, fmt.Errorf("claim %s: source %s names repository %s, whose commit cannot be read: %v", c.Name, source.Ref, source.Repo, err)
		}
		dirty, err := git(root, "status", "--porcelain", "--", anchors[i].File)
		if err != nil {
			return nil, fmt.Errorf("claim %s: source %s: %v", c.Name, source.Ref, err)
		}
		if dirty != "" {
			return nil, fmt.Errorf("claim %s: %s has uncommitted changes in %s; commit them, so the recorded commit holds what onboarding checks", c.Name, anchors[i].File, source.Repo)
		}
		repo, err := git(root, "remote", "get-url", "origin")
		if err != nil || repo == "" {
			repo = filepath.ToSlash(source.Repo)
		}
		sources = append(sources, grouping.Source{Ref: source.Ref, Repo: repo, Commit: commit})
	}
	return sources, nil
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// shellWord quotes one chain token so a chain built from it parses back to the same token.
func shellWord(token string) string {
	if token != "" && strings.IndexFunc(token, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("-_./:@+,=%", r)
	}) < 0 {
		return token
	}
	return "'" + strings.ReplaceAll(token, "'", `'"'"'`) + "'"
}
