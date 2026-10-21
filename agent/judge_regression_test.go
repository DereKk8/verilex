package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spaces inside a literal can change what the caller requires. The judge must see the exact
// text, and the changed literal must not reuse the earlier answer.
func TestLiteralWhitespaceReachesTheJudgeAndInvalidatesReuse(t *testing.T) {
	dir := t.TempDir()
	brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$HOME/started\"\n")
	log := filepath.Join(dir, "requests.jsonl")
	intents := []string{`prove output equals "ready now"`, `prove output equals "ready  now"`, `prove output equals "ready  now"`}
	for i, intent := range intents {
		fake := writeFake(t, dir, fakeFiles{ticket: intentTicket(intent)})
		args := []string{"--intent", intent, "--harness", "stub", "--model", "stub"}
		if i == 2 {
			args = append(args, "--judgments", filepath.Join(dir, "fresh"))
		}
		run := launchKeptEnv(t, dir, fake, brain, []string{"STANDIN_JUDGE_LOG=" + log}, args...)
		if run.code != 2 || run.stdout != "" || !strings.Contains(run.stderr, "answered none with confidence 0.90") {
			t.Fatalf("request %d: exit %d\nstdout: %s\nstderr: %s", i, run.code, run.stdout, run.stderr)
		}
		if _, err := os.Stat(filepath.Join(run.root, "brain", "home", "started")); !os.IsNotExist(err) {
			t.Fatalf("request %d started brain: %v", i, err)
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("judge saw %d requests, want 3:\n%s", len(lines), data)
	}
	for i, line := range lines {
		var got struct {
			Request string `json:"request"`
		}
		if err := json.Unmarshal([]byte(line), &got); err != nil || got.Request != intents[i] {
			t.Errorf("request %d = %q, want %q (%v)", i, got.Request, intents[i], err)
		}
	}
}

// A malformed dependency answer must neither start the brain nor enter the judgment store.
// Check both the raw protocol and a provider envelope, whose outer metadata remains allowed.
func TestJudgeAnswerSchemaIsEnforcedBeforeStartingTheBrain(t *testing.T) {
	for _, tc := range []struct{ name, answer string }{
		{"missing reason", `{"coverage":"full","claims":["store-opened"],"confidence":0.9}`},
		{"extra property", `{"coverage":"full","claims":["store-opened"],"confidence":0.9,"reason":"opens","trust_me":true}`},
		{"null reason", `{"coverage":"full","claims":["store-opened"],"confidence":0.9,"reason":null}`},
		{"numeric reason", `{"coverage":"full","claims":["store-opened"],"confidence":0.9,"reason":7}`},
		{"missing confidence", `{"coverage":"full","claims":["store-opened"],"reason":"opens"}`},
		{"null confidence", `{"coverage":"full","claims":["store-opened"],"confidence":null,"reason":"opens"}`},
		{"null claims", `{"coverage":"none","claims":null,"confidence":0.9,"reason":"none"}`},
		{"null claim", `{"coverage":"full","claims":[null],"confidence":0.9,"reason":"opens"}`},
		{"not an object", `null`},
	} {
		for _, envelope := range []bool{false, true} {
			name := tc.name
			answer := tc.answer
			if envelope {
				name += " in envelope"
				answer = `{"type":"result","is_error":false,"provider_metadata":"allowed","structured_output":` + answer + `}`
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				green := claimRun("green", "r1", []string{"store-opened"}, nil)
				fake := writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket("prove the store opens")})
				brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$HOME/started\"\nverilex run --json --claim store-opened\n")
				args := []string{"--intent", "prove the store opens", "--harness", "stub", "--model", "stub"}
				run := launchKeptEnv(t, dir, fake, brain, []string{"STANDIN_JUDGE_ANSWER=" + answer}, args...)
				if run.code != 2 || run.stdout != "" || !strings.Contains(run.stderr, "inconclusive: the judge answer breaks the schema") {
					t.Errorf("exit %d\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
				}
				if _, err := os.Stat(filepath.Join(run.root, "brain", "home", "started")); !os.IsNotExist(err) {
					t.Errorf("brain started on malformed answer: %v", err)
				}
				paths, err := filepath.Glob(filepath.Join(dir, "judgments", "*", "*.json"))
				if err != nil || len(paths) != 0 {
					t.Errorf("malformed answer stored: %v (%v)", paths, err)
				}
				// A valid provider answer is accepted and stored under the same intent and judge.
				valid := `{"type":"result","provider_metadata":42,"structured_output":{"coverage":"full","claims":["store-opened"],"confidence":0.9,"reason":"opens"}}`
				control := launchKeptEnv(t, dir, fake, brain, []string{"STANDIN_JUDGE_ANSWER=" + valid}, args...)
				if control.code != 0 || control.stdout != string(green) || control.home(t, "started") != "" {
					t.Fatalf("valid answer: exit %d\nstdout: %s\nstderr: %s", control.code, control.stdout, control.stderr)
				}
				paths, err = filepath.Glob(filepath.Join(dir, "judgments", "*", "*.json"))
				if err != nil || len(paths) != 1 {
					t.Fatalf("valid judgment files: %v (%v)", paths, err)
				}
				data, err := os.ReadFile(paths[0])
				var stored struct {
					Reason string `json:"reason"`
				}
				if err != nil || json.Unmarshal(data, &stored) != nil || stored.Reason != "opens" {
					t.Fatalf("stored answer: %s (%v)", data, err)
				}
			})
		}
	}
}

// A delayed full must apply an already recorded none before it starts a brain. Both launchers
// miss the cache, but the none finishes before the full is allowed to answer.
func TestConcurrentJudgmentsApplyTheStoredCautiousAnswer(t *testing.T) {
	dir := t.TempDir()
	intent := "prove the store opens"
	green := claimRun("green", "r1", []string{"store-opened"}, nil)
	fake := writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket(intent)})
	brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$HOME/started\"\nverilex run --json --claim store-opened\n")
	path := filepath.Join(dir, "concurrent-judge")
	script := `#!/usr/bin/env python3
import json, os, pathlib, sys, time
sys.stdin.read()
gate = pathlib.Path(os.environ["JUDGE_GATE"])
role = os.environ["JUDGE_ROLE"]
with (gate / "calls").open("a") as log:
    log.write(role + "\n")
if role == "full":
    (gate / "waiting").touch()
    deadline = time.monotonic() + 15
    while not (gate / "release").exists():
        if time.monotonic() > deadline:
            sys.exit("release never arrived")
        time.sleep(0.01)
print(json.dumps({"coverage":role,"claims":["store-opened"] if role == "full" else [],"confidence":0.9,"reason":"controlled " + role}))
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	args := []string{"--judge", path, "--intent", intent, "--harness", "stub", "--model", "stub"}
	// Prepare the shared project and skill before either launcher starts.
	if _, err := launcherArgs(dir, fake, brain, args...); err != nil {
		t.Fatal(err)
	}
	tmp := runTemp(t)
	done := make(chan kept, 1)
	go func() {
		out, stderr, code, err := launchEnv(dir, fake, brain, []string{"JUDGE_GATE=" + dir, "JUDGE_ROLE=full", "TMPDIR=" + tmp}, append(args, "--keep-work")...)
		if err != nil {
			stderr += err.Error()
			code = -1
		}
		done <- kept{stdout: out, stderr: stderr, code: code}
	}()
	// Always release the judge, including when the test's preconditions fail.
	defer os.WriteFile(filepath.Join(dir, "release"), nil, 0o600)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "waiting")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the full judge never reached the cache-miss gate")
		}
		time.Sleep(10 * time.Millisecond)
	}
	none := launchKeptEnv(t, dir, fake, brain, []string{"JUDGE_GATE=" + dir, "JUDGE_ROLE=none"}, args...)
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	full := <-done
	full.root = runRoot(t, tmp)
	after := launchKeptEnv(t, dir, fake, brain, []string{"JUDGE_GATE=" + dir, "JUDGE_ROLE=full"}, args...)
	for name, run := range map[string]kept{"none": none, "delayed full": full, "after": after} {
		if run.code != 2 || run.stdout != "" || !strings.Contains(run.stderr, "answered none with confidence 0.90: controlled none") {
			t.Errorf("%s: exit %d\nstdout: %s\nstderr: %s", name, run.code, run.stdout, run.stderr)
		}
		if _, err := os.Stat(filepath.Join(run.root, "brain", "home", "started")); !os.IsNotExist(err) {
			t.Errorf("%s brain started: %v", name, err)
		}
	}
	if calls, _ := os.ReadFile(filepath.Join(dir, "calls")); string(calls) != "full\nnone\n" {
		t.Fatalf("judge calls %q, want full then none, with after-run reuse", calls)
	}
	control := launchKeptEnv(t, dir, fake, brain, []string{"JUDGE_GATE=" + dir, "JUDGE_ROLE=full"}, append(args, "--judgments", filepath.Join(dir, "fresh"))...)
	if control.code != 0 || control.stdout != string(green) || control.home(t, "started") != "" {
		t.Fatalf("uncontested full: exit %d\nstdout: %s\nstderr: %s", control.code, control.stdout, control.stderr)
	}
}

// An explicit judge path is literal, even when its filename contains a template placeholder.
// Its answer must not be supplied by another executable or poison reuse by the genuine judge.
func TestLiteralJudgePathCannotPoisonReuse(t *testing.T) {
	dir := t.TempDir()
	green := claimRun("green", "r1", []string{"store-opened"}, nil)
	fake := writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket(trap)})
	brain := writeBrain(t, dir, "#!/bin/sh\ntouch \"$HOME/started\"\nverilex run --json --claim store-opened\n")
	script, err := os.ReadFile(standinJudge())
	if err != nil {
		t.Fatal(err)
	}
	literal := filepath.Join(dir, "judge-{model}")
	if err = os.WriteFile(literal, script, 0o700); err != nil {
		t.Fatal(err)
	}
	wrong := "#!/bin/sh\necho '{\"coverage\":\"full\",\"claims\":[\"store-opened\"],\"confidence\":0.9,\"reason\":\"wrong executable\"}'\n"
	if err = os.WriteFile(filepath.Join(dir, "judge-claude-opus-5-5"), []byte(wrong), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{literal, standinJudge()} {
		run := launchKept(t, dir, fake, brain, "--judge", path, "--intent", trap, "--harness", "stub", "--model", "stub")
		if run.code != 2 || run.stdout != "" || !strings.Contains(run.stderr, `intent "refund an added item" names no claim`) {
			t.Fatalf("judge %s: exit %d\nstdout: %s\nstderr: %s", path, run.code, run.stdout, run.stderr)
		}
		if _, err = os.Stat(filepath.Join(run.root, "brain", "home", "started")); !os.IsNotExist(err) {
			t.Fatalf("brain started for the trap: %v", err)
		}
	}
	// The same literal executable still lets a genuinely covered request through.
	fake = writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket("prove the store opens")})
	run := launchKept(t, dir, fake, brain, "--judge", literal, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	if run.code != 0 || run.stdout != string(green) || run.home(t, "started") != "" {
		t.Fatalf("covered request: exit %d\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
	}
}
