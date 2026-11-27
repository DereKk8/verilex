package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Onboarding records the word files its trials ran with, so a change to them while it runs records
// nothing, never the files as they are once the trials are over.
func TestWordChangedDuringOnboardingRecordsNothing(t *testing.T) {
	root := product(t)
	used(t, root, "store-open")
	used(t, root, "store-open")
	launch := filepath.Join(root, ".verilex", "frame", "launch")
	original := read(t, launch)
	// Each launch leaves a line in the word's own directory, as an edit during the trials would.
	write(t, launch, strings.Replace(original, "run = os.environ[\"VERILEX_RUN\"]\n", "run = os.environ[\"VERILEX_RUN\"]\nwith open(Path(os.environ[\"VERILEX_PROJECT_ROOT\"], \".verilex\", \"words\", \"store-open\", \"launches.log\"), \"a\") as log:\n    log.write(run + \"\\n\")\n", 1), 0o755)
	equal(t, verilex(t, root, nil, "onboard", "store-open"), output{code: 2, stderr: "verilex: refused: the word's files changed while store-open was onboarded; onboard it again\n"})
	equal(t, status(t, root, "store-open"), "provisional")
}

// installSwapper wires frame/launch and frame/cleanup so that while gate exists, every active
// trial or run swaps target to during (using mode "inplace" or "rename") on the first launch and
// restores target to original (including its mtime) on the last cleanup. A lockfile with a
// reference count makes parallel onboarding trials deterministic.
func installSwapper(t *testing.T, root, target, during, mode string) (enable, disable func()) {
	t.Helper()
	scratch := filepath.Join(filepath.Dir(root), "swap")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	duringFile := filepath.Join(scratch, "during")
	write(t, duringFile, during, 0o755)
	gate := filepath.Join(scratch, "enabled")
	swapPy := filepath.Join(scratch, "swap.py")
	script := `#!/usr/bin/env python3
import fcntl, json, os, sys
from pathlib import Path

action = sys.argv[1]
scratch = Path(sys.argv[2])
target = Path(sys.argv[3])
during = Path(sys.argv[4])
mode = sys.argv[5]
gate = scratch / "enabled"
state_file = scratch / "state.json"
backup_file = scratch / "backup"

with open(scratch / "lock", "a+") as lock:
    fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
    state = json.loads(state_file.read_text()) if state_file.exists() else {"active": 0}
    if action == "enter":
        if not gate.exists():
            sys.exit(0)
        if state["active"] == 0:
            st = target.stat()
            backup_file.write_bytes(target.read_bytes())
            state["atime_ns"] = st.st_atime_ns
            state["mtime_ns"] = st.st_mtime_ns
            state["mode"] = st.st_mode & 0o7777
            data = during.read_bytes()
            if mode == "rename":
                tmp = target.parent / (target.name + ".swap")
                tmp.write_bytes(data)
                os.chmod(tmp, state["mode"])
                os.replace(tmp, target)
            else:
                with open(target, "wb") as f:
                    f.write(data)
        state["active"] += 1
        state_file.write_text(json.dumps(state))
    elif action == "leave":
        if state["active"] <= 0:
            sys.exit(0)
        state["active"] -= 1
        if state["active"] == 0:
            orig = backup_file.read_bytes()
            if mode == "rename":
                tmp = target.parent / (target.name + ".restore")
                tmp.write_bytes(orig)
                os.chmod(tmp, state["mode"])
                os.utime(tmp, ns=(state["atime_ns"], state["mtime_ns"]))
                os.replace(tmp, target)
            else:
                with open(target, "wb") as f:
                    f.write(orig)
                os.utime(target, ns=(state["atime_ns"], state["mtime_ns"]))
        state_file.write_text(json.dumps(state))
`
	write(t, swapPy, script, 0o700)

	launch := filepath.Join(root, ".verilex", "frame", "launch")
	launchCode := read(t, launch)
	hookEnter := fmt.Sprintf("import subprocess\nsubprocess.run([%q, 'enter', %q, %q, %q, %q], check=True)\n", swapPy, scratch, target, duringFile, mode)
	write(t, launch, strings.Replace(launchCode, "run = os.environ[\"VERILEX_RUN\"]\n", "run = os.environ[\"VERILEX_RUN\"]\n"+hookEnter, 1), 0o755)

	cleanup := filepath.Join(root, ".verilex", "frame", "cleanup")
	cleanupCode := read(t, cleanup)
	hookLeave := fmt.Sprintf("\nimport subprocess\nsubprocess.run([%q, 'leave', %q, %q, %q, %q], check=True)\n", swapPy, scratch, target, duringFile, mode)
	write(t, cleanup, cleanupCode+hookLeave, 0o755)

	return func() { write(t, gate, "1\n", 0o600) }, func() { _ = os.Remove(gate) }
}

// A broken word that misses its planted defect must never be admitted even if its files are
// swapped to a passing implementation while onboarding trials run and restored (with original
// mtime) before onboarding finishes.
func TestWordChangedAndRestoredDuringOnboardingIsNotAdmitted(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			run := filepath.Join(root, ".verilex", "words", "store-open", "run")
			good := read(t, run)
			broken := "#!/bin/sh\ncat >/dev/null\necho '{\"verdict\": \"pass\", \"observation\": \"store.json holds no items\"}'\n"
			used(t, root, "store-open")
			used(t, root, "store-open")
			write(t, run, broken, 0o755)

			enable, _ := installSwapper(t, root, run, good, mode)
			enable()
			done := verilex(t, root, nil, "onboard", "store-open")
			if done.code == 0 {
				t.Fatalf("broken word was admitted after ABA swap (%s): %+v", mode, done)
			}
			equal(t, status(t, root, "store-open"), "provisional")
		})
	}
}

// Even when the word on disk would pass on its own, changing its files and restoring them (with
// original mtime) while onboarding runs must refuse to record an admission.
func TestWordEditedAndRestoredDuringOnboardingRecordsNothing(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			run := filepath.Join(root, ".verilex", "words", "store-open", "run")
			good := read(t, run)
			other := good + "\n# swapped during onboarding\n"
			used(t, root, "store-open")
			used(t, root, "store-open")

			enable, _ := installSwapper(t, root, run, other, mode)
			enable()
			done := verilex(t, root, nil, "onboard", "store-open")
			equal(t, done, output{code: 2, stderr: "verilex: refused: the word's files changed while store-open was onboarded; onboard it again\n"})
			equal(t, status(t, root, "store-open"), "provisional")
		})
	}
}

// Changing a shared word file and restoring it (with original mtime) while onboarding runs must
// refuse to record an admission.
func TestBindingChangedAndRestoredDuringOnboardingRecordsNothing(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			helper := filepath.Join(root, ".verilex", "words", "tally_word.py")
			orig := read(t, helper)
			other := orig + "\n# swapped shared helper\n"
			used(t, root, "store-open")
			used(t, root, "store-open")

			enable, _ := installSwapper(t, root, helper, other, mode)
			enable()
			done := verilex(t, root, nil, "onboard", "store-open")
			equal(t, done, output{code: 2, stderr: "verilex: refused: the frame or the shared word files changed while store-open was onboarded; onboard it again\n"})
			equal(t, status(t, root, "store-open"), "provisional")
		})
	}
}

// If a declared product input changes from broken state A to healthy state B while a run executes
// and is restored to A (with original mtime) before the run finishes, no ledger pass may be
// recorded for A: the next run must run live and catch broken A, never skip green.
func TestInputChangedAndRestoredDuringRunIsNotRecordedInLedger(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			tally := filepath.Join(root, "bin", "tally")
			healthy := read(t, tally)
			broken := strings.Replace(healthy, `defect = os.environ.get("TALLY_DEFECT")`, `defect = os.environ.get("TALLY_DEFECT") or "drop-adds"`, 1)
			if broken == healthy {
				t.Fatal("could not plant defect in bin/tally")
			}

			enable, disable := installSwapper(t, root, tally, healthy, mode)
			admitted(t, root)
			write(t, tally, broken, 0o755)

			enable()
			first := green(t, root, nil, chain)
			ranLive(t, first)
			disable()

			if got := plan(t, root, chain).stdout; got != "plan: skip 0, run 3; store-open: no green result on record\n" {
				t.Fatalf("ABA input edit recorded a reusable pass (%s): %q", mode, got)
			}
			// Running honestly against broken bin/tally must run live and report red, never skip green.
			second, rec := runJSON(t, root, nil, chain)
			equal(t, second.code, 1)
			equal(t, string(*rec.Verdict), "red")
			ranLive(t, rec)
		})
	}
}

// If a word's run script changes from broken state A to healthy state B while a run executes and is
// restored to A (with original mtime) before the run finishes, no ledger pass may stand for A.
func TestWordChangedAndRestoredDuringRunIsNotRecordedInLedger(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			wordRun := filepath.Join(root, ".verilex", "words", "item-stored", "run")
			healthy := read(t, wordRun)
			broken := "#!/bin/sh\ncat >/dev/null\necho '{\"verdict\": \"fail\", \"preconditions_held\": true, \"detail\": \"broken word A\"}'\nexit 1\n"

			enable, disable := installSwapper(t, root, wordRun, healthy, mode)
			write(t, wordRun, broken, 0o755)
			admitted(t, root)

			enable()
			first, _ := runJSON(t, root, nil, chain)
			disable()
			_ = first

			if got := plan(t, root, chain).stdout; strings.HasPrefix(got, "plan: skip 3, run 0") {
				t.Fatalf("ABA word edit recorded a reusable pass for broken word A (%s): %q", mode, got)
			}
			second, rec := runJSON(t, root, nil, chain)
			equal(t, second.code, 1)
			equal(t, string(*rec.Verdict), "red")
			ranLive(t, rec)
		})
	}
}

// The same ABA protection applies to a kept instance's history settled for --continue: if an input
// changes to B while the --keep run executes and is restored to A (with original mtime) before the
// run finishes, settle must drop its history stamp so --continue refuses to rely on it.
func TestInputChangedAndRestoredDuringKeepIsNotReusedByContinue(t *testing.T) {
	for _, mode := range []string{"inplace", "rename"} {
		t.Run(mode, func(t *testing.T) {
			root := product(t)
			tally := filepath.Join(root, "bin", "tally")
			healthy := read(t, tally)
			broken := strings.Replace(healthy, `defect = os.environ.get("TALLY_DEFECT")`, `defect = os.environ.get("TALLY_DEFECT") or "drop-adds"`, 1)
			if broken == healthy {
				t.Fatal("could not plant defect in bin/tally")
			}

			// Swap bin/tally to healthy on launch, and restore broken (with original mtime) inside
			// item-listed right before it exits, since --keep runs no cleanup.
			scratch := filepath.Join(filepath.Dir(root), "swap-keep")
			if err := os.MkdirAll(scratch, 0o700); err != nil {
				t.Fatal(err)
			}
			healthyFile := filepath.Join(scratch, "healthy")
			write(t, healthyFile, healthy, 0o755)
			backupFile := filepath.Join(scratch, "backup")
			metaFile := filepath.Join(scratch, "meta.json")
			launch := filepath.Join(root, ".verilex", "frame", "launch")
			enterHook := fmt.Sprintf(`import json as _j
_t = Path(%q)
_st = _t.stat()
Path(%q).write_bytes(_t.read_bytes())
Path(%q).write_text(_j.dumps({"a": _st.st_atime_ns, "m": _st.st_mtime_ns, "p": _st.st_mode & 0o7777}))
if %q == "rename":
    _tmp = _t.parent / (_t.name + ".swap")
    _tmp.write_bytes(Path(%q).read_bytes())
    os.chmod(_tmp, _st.st_mode & 0o7777)
    os.replace(_tmp, _t)
else:
    _t.write_bytes(Path(%q).read_bytes())
`, tally, backupFile, metaFile, mode, healthyFile, healthyFile)
			write(t, launch, strings.Replace(read(t, launch), "run = os.environ[\"VERILEX_RUN\"]\n", "run = os.environ[\"VERILEX_RUN\"]\n"+enterHook, 1), 0o755)

			listed := filepath.Join(root, ".verilex", "words", "item-listed", "run")
			leaveHook := fmt.Sprintf(`import json as _j, os as _os
_t = Path(%q)
_m = _j.loads(Path(%q).read_text())
_orig = Path(%q).read_bytes()
if %q == "rename":
    _tmp = _t.parent / (_t.name + ".restore")
    _tmp.write_bytes(_orig)
    _os.chmod(_tmp, _m["p"])
    _os.utime(_tmp, ns=(_m["a"], _m["m"]))
    _os.replace(_tmp, _t)
else:
    _t.write_bytes(_orig)
    _os.utime(_t, ns=(_m["a"], _m["m"]))
`, tally, metaFile, backupFile, mode)
			write(t, listed, strings.Replace(read(t, listed), `result("pass"`, leaveHook+`result("pass"`, 1), 0o755)

			admitted(t, root)
			write(t, tally, broken, 0o755)

			kept := green(t, root, nil, chain, "--keep")
			defer verilex(t, root, nil, "cleanup", kept.Run)

			// --continue must NOT skip on the kept instance's history whose input changed A->B->A.
			done := plan(t, root, chain, "--continue", kept.Run)
			if done.code == 0 {
				t.Fatalf("plan --continue reused a kept history entry after ABA input edit (%s): %+v", mode, done)
			}
			contains(t, done.stderr, "the kept instance already holds the effects")
		})
	}
}
