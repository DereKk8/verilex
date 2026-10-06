package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DereKk8/verilex/internal/runner"
	"github.com/DereKk8/verilex/internal/ticket"
)

func userProfiles(root string) string {
	return filepath.Join(filepath.Dir(root), "config", "verilex", "profiles.yaml")
}

func projectProfiles(root string) string { return filepath.Join(root, ".verilex", "profiles.yaml") }

// writeFile writes text at path, creating its directory; an empty text removes the file.
func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if text == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, path, text, 0644)
}

// ticketAt writes a ticket file beside the product, never inside it, and returns its path.
func ticketAt(t *testing.T, root, name, text string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(root), "tickets", name+".yaml")
	writeFile(t, path, text)
	return path
}

func resolved(t *testing.T, root, path string) ticket.Ticket {
	t.Helper()
	done := verilex(t, root, nil, "ticket", path, "--json")
	if done.code != 0 {
		t.Fatalf("ticket refused: %+v", done)
	}
	var got ticket.Ticket
	if err := json.Unmarshal([]byte(done.stdout), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// line returns "  <key>: <value>\n" for a YAML mapping entry, or nothing when value is empty.
func line(indent, key, value string) string {
	if value == "" {
		return ""
	}
	return indent + key + ": " + value + "\n"
}

// Rule: each field of a run ticket comes from the highest level that sets it: the ticket, its
// named profile, the project default, the user default, then the built-in.
func TestEachTicketLevelWinsOverTheLevelsBelow(t *testing.T) {
	root := product(t)
	levels := func(ticketEffort, profileEffort, projectEffort, userEffort string) string {
		writeFile(t, userProfiles(root), "defaults:\n  profile: quick-verify\n  harness: user-harness\n  model: user-model\n"+
			line("  ", "effort", userEffort)+"  token_budget: 1000\n  time_budget: 10m\n"+
			"profiles:\n  quick-verify:\n    model: user-quick-model\n    effort: minimal\n"+
			"  deep-verify:\n    harness: user-deep-harness\n    model: user-deep-model\n")
		writeFile(t, projectProfiles(root), "defaults:\n  profile: deep-verify\n  model: project-model\n"+line("  ", "effort", projectEffort)+"  time_budget: 20m\n"+
			"profiles:\n  deep-verify:\n    model: deep-model\n"+line("    ", "effort", profileEffort))
		return ticketAt(t, root, "precedence", "intent: prove a renamed item keeps its count\n"+line("", "effort", ticketEffort))
	}

	cases := []struct {
		name, ticketEffort, profileEffort, projectEffort, userEffort string
		effort, from                                                 string
	}{
		{"ticket", "max", "xhigh", "high", "low", "max", "ticket"},
		{"named profile", "", "xhigh", "high", "low", "xhigh", "project profile deep-verify"},
		{"project default", "", "", "high", "low", "high", "project default"},
		{"user default", "", "", "", "low", "low", "user default"},
		{"built-in", "", "", "", "", "medium", "built-in"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolved(t, root, levels(c.ticketEffort, c.profileEffort, c.projectEffort, c.userEffort))
			equal(t, [2]string{got.Effort, got.From["effort"]}, [2]string{c.effort, c.from})
		})
	}

	// Every field resolves on its own. The project's deep-verify shadows the user's whole
	// profile of that name, so the harness falls through to the user default.
	path := levels("max", "xhigh", "high", "low")
	equal(t, resolved(t, root, path), ticket.Ticket{
		Intent: "prove a renamed item keeps its count", Profile: "deep-verify",
		Harness: "user-harness", Model: "deep-model", Effort: "max", TokenBudget: 1000, TimeBudget: "20m",
		From: map[string]string{
			"profile": "project default", "harness": "user default", "model": "project profile deep-verify",
			"effort": "ticket", "token_budget": "user default", "time_budget": "project default",
		},
	})
	equal(t, verilex(t, root, nil, "ticket", path), output{0, "intent: prove a renamed item keeps its count\n" +
		"profile: deep-verify  from project default\n" +
		"harness: user-harness  from user default\n" +
		"model: deep-model  from project profile deep-verify\n" +
		"effort: max  from ticket\n" +
		"token_budget: 1000  from user default\n" +
		"time_budget: 20m  from project default\n", ""})

	// A ticket's profile wins over the defaults' choice, and resolves from the user's profiles
	// when the project has none of that name.
	path = ticketAt(t, root, "quick", "diff: main...HEAD\nprofile: quick-verify\n")
	got := resolved(t, root, path)
	equal(t, [4]string{got.Diff, got.Model, got.Effort, got.From["model"]}, [4]string{"main...HEAD", "user-quick-model", "minimal", "user profile quick-verify"})

	// Without a project default profile, the user default picks it.
	writeFile(t, projectProfiles(root), "defaults:\n  model: project-model\n")
	got = resolved(t, root, ticketAt(t, root, "plain", "intent: prove listing works\n"))
	equal(t, [4]string{got.Profile, got.From["profile"], got.Model, got.From["model"]}, [4]string{"quick-verify", "user default", "user-quick-model", "user profile quick-verify"})
}

// Rule: named profiles resolve to a concrete harness, model and effort.
func TestNamedProfilesResolveToHarnessModelAndEffort(t *testing.T) {
	root := product(t)
	writeFile(t, projectProfiles(root), "profiles:\n"+
		"  quick-verify: {harness: codex, model: gpt-5.1-codex-mini, effort: low, time_budget: 5m}\n"+
		"  deep-verify: {harness: claude-code, model: claude-opus-5-5, effort: xhigh, token_budget: 400000, time_budget: 1h}\n")
	for name, want := range map[string][5]any{
		"quick-verify": {"codex", "gpt-5.1-codex-mini", "low", 0, "5m"},
		"deep-verify":  {"claude-code", "claude-opus-5-5", "xhigh", 400000, "1h"},
	} {
		got := resolved(t, root, ticketAt(t, root, name, "intent: prove the store opens empty\nprofile: "+name+"\n"))
		equal(t, [5]any{got.Harness, got.Model, got.Effort, got.TokenBudget, got.TimeBudget}, want)
		for _, field := range []string{"harness", "model", "effort"} {
			equal(t, got.From[field], "project profile "+name)
		}
	}
}

// Rule: an invalid ticket is refused before anything starts, naming the file and the field.
func TestInvalidTicketIsRefusedNamingTheField(t *testing.T) {
	base := "defaults:\n  harness: claude-code\n  model: claude-opus-5-5\n"
	cases := []struct {
		name, ticket, project, user, refusal string
	}{
		{"effort", "intent: x\neffort: turbo\n", "", base, `effort: "turbo" is not one of minimal, low, medium, high, xhigh, max`},
		{"unknown field", "intent: x\nefort: max\n", "", base, "efort: unknown field; expected one of intent, diff, profile, harness, model, effort, token_budget, time_budget"},
		{"token budget", "intent: x\ntoken_budget: -5\n", "", base, "token_budget: must be a positive whole number of tokens, not -5"},
		{"time budget", "intent: x\ntime_budget: soon\n", "", base, `time_budget: must be a positive duration such as 30m or 1h30m, not "soon"`},
		{"no intent or diff", "model: claude-opus-5-5\n", "", base, "intent or diff: a ticket names at least one"},
		{"diff", "diff: --output=/tmp/x\n", "", base, `diff: must be a git revision or range such as main...HEAD, not "--output=/tmp/x"`},
		{"model", "intent: x\nmodel: two words\n", "", base, `model: must be a plain name without spaces, not "two words"`},
		{"unknown profile", "intent: x\nprofile: nightly\n", "", base, "profile: no profile named nightly in {project} or {user}"},
		{"harness unset", "intent: x\nmodel: claude-opus-5-5\n", "", "", "harness: not set by the ticket, a profile, or the project or user defaults"},
		{"profiles file", "intent: x\nprofile: quick-verify\n", "profiles:\n  quick-verify:\n    model: two words\n", base, `{project}: profiles.quick-verify.model: must be a plain name without spaces, not "two words"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := product(t)
			writeFile(t, projectProfiles(root), c.project)
			writeFile(t, userProfiles(root), c.user)
			path := ticketAt(t, root, "bad", c.ticket)
			project, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			refusal := strings.NewReplacer("{project}", projectProfiles(project), "{user}", userProfiles(root)).Replace(c.refusal)
			if !strings.HasPrefix(refusal, projectProfiles(project)) {
				refusal = "ticket " + path + ": " + refusal
			}
			want := output{2, "", "verilex: refused: " + refusal + "\n"}
			equal(t, verilex(t, root, nil, "ticket", path), want)
			equal(t, verilex(t, root, nil, "plan", chain, "--ticket", path), want)
			equal(t, verilex(t, root, nil, "run", chain, "--ticket", path), want)
			equal(t, runCount(t, root), 0)
			stores, err := os.ReadDir(filepath.Join(filepath.Dir(root), "stores"))
			if err != nil {
				t.Fatal(err)
			}
			equal(t, len(stores), 0)
		})
	}
}

// Rule: the ticket rides along. It never changes what is skipped, and plan and run report it.
func TestTicketIsCarriedWithoutChangingTheSkipDecision(t *testing.T) {
	root := curated(t)
	writeFile(t, projectProfiles(root), "defaults:\n  harness: claude-code\n  model: claude-opus-5-5\n")
	quick := ticketAt(t, root, "quick", "intent: prove items are listed\neffort: low\n")
	deep := ticketAt(t, root, "deep", "intent: prove items are listed\neffort: max\n")

	first := green(t, root, nil, chain, "--ticket", quick)
	ranLive(t, first)
	equal(t, first.Ticket.Effort, "low")
	equal(t, recordOf(t, root, first.Run).Ticket.Effort, "low")

	equal(t, plan(t, root, chain, "--ticket", deep).stdout, plan(t, root, chain).stdout)
	var decided runner.Plan
	if err := json.Unmarshal([]byte(plan(t, root, chain, "--ticket", deep, "--json").stdout), &decided); err != nil {
		t.Fatal(err)
	}
	equal(t, [2]string{decided.Rerun, decided.Ticket.Effort}, [2]string{"", "max"})

	second := green(t, root, nil, chain, "--ticket", deep)
	equal(t, second.Skipped, true)
	equal(t, second.Words[0].ReliesOn, first.Run)
	equal(t, recordOf(t, root, second.Run).Ticket.Effort, "max")
	// A run without a ticket records none.
	equal(t, green(t, root, nil, chain).Ticket, (*ticket.Ticket)(nil))
}

// Rule: concurrent runs with different tickets each carry their own and change no shared file:
// not the tickets, the profiles, nor the product.
func TestConcurrentRunsWithDifferentTicketsShareNoTicketState(t *testing.T) {
	root := product(t)
	// Each run waits inside its word until the other run's word has started, so both are live at once.
	met := filepath.Join(filepath.Dir(root), "met")
	if err := os.Mkdir(met, 0755); err != nil {
		t.Fatal(err)
	}
	addWord(t, root, "store-met", `touch "$MET/$VERILEX_RUN"
i=0
while [ "$(ls "$MET" | wc -l)" -lt 2 ]; do
  i=$((i+1)); if [ $i -gt 400 ]; then echo '{"verdict": "blocked", "detail": "the other run never started"}'; exit 2; fi
  sleep 0.05
done
echo '{"verdict": "pass", "observation": "both runs were live at once"}'
`)
	writeFile(t, userProfiles(root), "defaults:\n  harness: claude-code\n  model: claude-sonnet-5-5\n")
	writeFile(t, projectProfiles(root), "profiles:\n  quick-verify: {model: claude-haiku-4-5, effort: low}\n  deep-verify: {model: claude-opus-5-5, effort: max}\n")
	tickets := map[string]string{
		"quick-verify": ticketAt(t, root, "quick", "intent: prove the store opens\nprofile: quick-verify\n"),
		"deep-verify":  ticketAt(t, root, "deep", "diff: main...HEAD\nprofile: deep-verify\n"),
	}
	before := []map[string]string{tree(t, root), tree(t, filepath.Join(filepath.Dir(root), "config")), tree(t, filepath.Join(filepath.Dir(root), "tickets"))}

	type started struct {
		cmd    *exec.Cmd
		stdout *strings.Builder
	}
	runs := map[string]started{}
	for profile, path := range tickets {
		cmd, stdout, _ := command(root, map[string]string{"MET": met}, "run", "store-open | store-met", "--ticket", path, "--json")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		runs[profile] = started{cmd, stdout}
	}
	for profile, run := range runs {
		if err := run.cmd.Wait(); err != nil {
			t.Fatalf("%s: %v: %s", profile, err, run.stdout)
		}
		var record runner.Record
		if err := json.Unmarshal([]byte(run.stdout.String()), &record); err != nil {
			t.Fatal(err)
		}
		equal(t, string(*record.Verdict), "green")
		saved := recordOf(t, root, record.Run).Ticket
		equal(t, [3]string{saved.Profile, saved.Harness, saved.From["model"]}, [3]string{profile, "claude-code", "project profile " + profile})
	}
	equal(t, recordOf(t, root, lastRunOf(t, root, func(r runner.Record) bool { return r.Ticket.Profile == "deep-verify" }).Run).Ticket.Model, "claude-opus-5-5")
	equal(t, recordOf(t, root, lastRunOf(t, root, func(r runner.Record) bool { return r.Ticket.Profile == "quick-verify" }).Run).Ticket.Model, "claude-haiku-4-5")
	met2, err := os.ReadDir(met)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, len(met2), 2)
	after := []map[string]string{tree(t, root), tree(t, filepath.Join(filepath.Dir(root), "config")), tree(t, filepath.Join(filepath.Dir(root), "tickets"))}
	equal(t, after, before)
}
