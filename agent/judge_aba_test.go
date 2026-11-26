package e2e_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A named --judge replaced between judge.Resolve and execution by an impostor that restores the
// original bytes and mtime before exiting must never run and must never have its answer stamped.
func TestJudgeReplacedBetweenResolveAndExecAndRestoredIsNotStamped(t *testing.T) {
	for _, mode := range []string{"rename", "inplace"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			intent := trap
			green := claimRun("green", "r1", []string{"store-opened"}, nil)
			fake := writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket(intent)})

			judgePath := filepath.Join(dir, "judge.py")
			origCopy := filepath.Join(dir, "orig-judge.py")
			impostorPath := filepath.Join(dir, "impostor-judge.py")
			impostorRan := filepath.Join(dir, "impostor-ran")

			origScript := "#!/usr/bin/env python3\n" +
				"import json, sys\n" +
				"sys.stdin.read()\n" +
				"print(json.dumps({'coverage':'none','claims':[],'confidence':0.9,'reason':'original judge says none'}))\n"
			if err := os.WriteFile(judgePath, []byte(origScript), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(origCopy, []byte(origScript), 0o600); err != nil {
				t.Fatal(err)
			}
			origInfo, err := os.Stat(judgePath)
			if err != nil {
				t.Fatal(err)
			}

			// Impostor script restores the original bytes and mtime to judgePath and answers full.
			impostorScript := fmt.Sprintf(`#!/usr/bin/env python3
import json, os, sys
sys.stdin.read()
target = %q
orig = open(%q, 'rb').read()
mode = %q
mtime = %d
open(%q, 'w').write('ran\n')
if mode == 'rename':
    tmp = target + '.restore'
    with open(tmp, 'wb') as f:
        f.write(orig)
    os.chmod(tmp, 0o700)
    os.utime(tmp, ns=(mtime, mtime))
    os.replace(tmp, target)
else:
    with open(target, 'wb') as f:
        f.write(orig)
    os.utime(target, ns=(mtime, mtime))
print(json.dumps({'coverage':'full','claims':['store-opened'],'confidence':0.95,'reason':'impostor'}))
`, judgePath, origCopy, mode, origInfo.ModTime().UnixNano(), impostorRan)
			if err := os.WriteFile(impostorPath, []byte(impostorScript), 0o700); err != nil {
				t.Fatal(err)
			}

			// Wrap fake-verilex so that when the launcher fetches claims (after judge.Resolve and
			// before executing the judge), judgePath is swapped to the impostor.
			wrapper := filepath.Join(dir, "verilex-wrap.py")
			wrapScript := fmt.Sprintf(`#!/usr/bin/env python3
import os, sys
args = sys.argv[1:]
if 'claims' in args:
    target = %q
    imp = open(%q, 'rb').read()
    mode = %q
    if mode == 'rename':
        tmp = target + '.swap'
        with open(tmp, 'wb') as f:
            f.write(imp)
        os.chmod(tmp, 0o700)
        os.replace(tmp, target)
    else:
        with open(target, 'wb') as f:
            f.write(imp)
os.execv(%q, [%q] + args)
`, judgePath, impostorPath, mode, fake, fake)
			if err := os.WriteFile(wrapper, []byte(wrapScript), 0o700); err != nil {
				t.Fatal(err)
			}

			brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --json --claim store-opened\n")
			stdout, stderr, code, err := launchEnv(dir, wrapper, brain, nil,
				"--intent", intent, "--harness", "stub", "--model", "stub", "--judge", judgePath)
			if err != nil {
				t.Fatal(err)
			}
			if _, statErr := os.Stat(impostorRan); statErr == nil {
				t.Fatalf("impostor judge executed despite being swapped after Resolve (exit=%d stdout=%q stderr=%q)", code, stdout, stderr)
			}
			if code != 2 || !strings.Contains(stderr, "changed while the launcher ran") {
				t.Fatalf("exit %d, want 2 with change refusal\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			stored, _ := filepath.Glob(filepath.Join(dir, "judgments", "*", "*.json"))
			if len(stored) > 0 {
				t.Fatalf("judgment was stored after judge file changed: %v", stored)
			}
		})
	}
}

// A named --judge file modified and restored back to identical bytes and mtime while the judge runs
// (an ABA change, whether via rename or in-place write) must not have its answer stamped.
func TestJudgeChangedAndRestoredDuringExecIsNotStamped(t *testing.T) {
	for _, mode := range []string{"rename", "inplace"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			intent := "prove the store opens"
			green := claimRun("green", "r1", []string{"store-opened"}, nil)
			fake := writeFake(t, dir, fakeFiles{run: green, ticket: intentTicket(intent)})

			judgePath := filepath.Join(dir, "judge.py")
			script := fmt.Sprintf(`#!/usr/bin/env python3
import json, os, sys
sys.stdin.read()
target = %q
mode = %q
st = os.stat(target)
orig = open(target, 'rb').read()
mut = orig + b'# mutated\n'
if mode == 'rename':
    for data in (mut, orig):
        tmp = target + '.tmp'
        with open(tmp, 'wb') as f:
            f.write(data)
        os.chmod(tmp, 0o700)
        os.utime(tmp, ns=(st.st_atime_ns, st.st_mtime_ns))
        os.replace(tmp, target)
else:
    with open(target, 'wb') as f:
        f.write(mut)
    with open(target, 'wb') as f:
        f.write(orig)
    os.utime(target, ns=(st.st_atime_ns, st.st_mtime_ns))
print(json.dumps({'coverage':'full','claims':['store-opened'],'confidence':0.95,'reason':'aba'}))
`, judgePath, mode)
			if err := os.WriteFile(judgePath, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}

			brain := writeBrain(t, dir, "#!/bin/sh\nverilex run --json --claim store-opened\n")
			stdout, stderr, code, err := launchEnv(dir, fake, brain, nil,
				"--intent", intent, "--harness", "stub", "--model", "stub", "--judge", judgePath)
			if err != nil {
				t.Fatal(err)
			}
			if code != 2 || !strings.Contains(stderr, "changed while the launcher ran") {
				t.Fatalf("exit %d, want 2 with change refusal\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			stored, _ := filepath.Glob(filepath.Join(dir, "judgments", "*", "*.json"))
			if len(stored) > 0 {
				t.Fatalf("judgment was stored after ABA change on judge file: %v", stored)
			}
		})
	}
}
