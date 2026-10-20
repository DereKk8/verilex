//go:build linux

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The same fixed brain exercises the documented Linux capabilities and refusal
// edges against the real launcher and core. Expected results live on the host,
// not in the brain. Existing brain/stop/rule tests cover terminal devices, time
// budgets, suggestion phases, leftover children and verdict ordering in depth.
func TestScriptedStandinBrain(t *testing.T) {
	for _, tc := range []struct {
		verdict, defect, locked string
		code                    int
	}{
		{"green", "", "", 0},
		{"red", "hide-lists", "", 1},
		{"inconclusive", "", "1", 2},
	} {
		t.Run(tc.verdict, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir, product, ledger := admittedProduct(t)
			marker := "standin-marker\n"
			put := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			put(filepath.Join(product, "standin-marker"), marker)
			if err := os.Symlink(coreBin, filepath.Join(product, "standin-core-link")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", filepath.Dir(coreBin)+":"+os.Getenv("PATH"))
			if err := commitAll(product); err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(dir, "state")
			callerHome, callerLedger := filepath.Join(dir, "caller-home"), filepath.Join(dir, "caller-ledger")
			// launcherArgs gives the launcher this judgments store.
			judgments := filepath.Join(dir, "judgments")
			for _, target := range []string{home, ledger, callerHome, callerLedger, judgments} {
				put(filepath.Join(target, "standin-marker"), marker)
			}
			credential := filepath.Join(os.Getenv("HOME"), ".standin", "auth")
			otherHome := filepath.Join(os.Getenv("HOME"), "other")
			put(credential, marker)
			put(otherHome, marker)

			// Use a synthetic harness to exercise credential and install-directory
			// mounts too. It is only the script below; no model or login is involved.
			install := t.TempDir()
			body, err := os.ReadFile(filepath.Join(repo, "agent", "testdata", "standin-brain.py"))
			if err != nil {
				t.Fatal(err)
			}
			brain := writeBrain(t, install, string(body))
			cases := standinCommands(coreBin, agentBin)
			commands := filepath.Join(install, "commands.json")
			data, err := json.Marshal(cases)
			if err != nil {
				t.Fatal(err)
			}
			put(commands, string(data))
			templates := filepath.Join(dir, "harnesses.json")
			data, err = json.Marshal(map[string]any{"standin": map[string]any{"argv": []string{brain}, "files": []string{"~/.standin/auth"}}})
			if err != nil {
				t.Fatal(err)
			}
			put(templates, string(data))

			canary, hits := listen(t, "tcp", "127.0.0.1:0")
			port := canary[strings.LastIndex(canary, ":")+1:]
			sockDir, err := os.MkdirTemp("/tmp", "vx-standin-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(sockDir) })
			hostSock, sockHits := listen(t, "unix", filepath.Join(sockDir, "host.sock"))
			// The lookalike must sit under /tmp whatever TMPDIR is: the brain's private /tmp is the only writable path there.
			outside := filepath.Join("/tmp", fmt.Sprintf("vx-standin-escaped-%d-%s", os.Getpid(), port))
			t.Cleanup(func() { os.Remove(outside) })
			abstract := fmt.Sprintf("vx-standin-%d-%s", os.Getpid(), port)
			_, abstractHits := listen(t, "unix", "\x00"+abstract)
			namespaces := map[string]string{}
			for _, name := range []string{"user", "mnt", "pid", "net", "ipc", "uts", "cgroup"} {
				id, err := os.Readlink("/proc/self/ns/" + name)
				if err != nil {
					t.Fatal(err)
				}
				namespaces[name] = id
			}
			data, err = json.Marshal(namespaces)
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]string{
				"STANDIN_NAMESPACES": string(data),
				"STANDIN_LAUNCHER":   agentBin, "STANDIN_COMMANDS": commands,
				"STANDIN_LEDGER": ledger, "STANDIN_STATE": home,
				"STANDIN_VERILEX_HOME": callerHome, "STANDIN_VERILEX_LEDGER": callerLedger, "STANDIN_JUDGMENTS": judgments,
				"VERILEX_HOME": callerHome, "VERILEX_LEDGER": callerLedger,
				"STANDIN_OTHER_HOME": otherHome, "STANDIN_OUTSIDE": outside,
				"STANDIN_HOST_PID": strconv.Itoa(os.Getpid()), "STANDIN_HOST_PORT": port,
				"STANDIN_HOST_SOCKET": hostSock, "STANDIN_HOST_ABSTRACT": abstract,
				"TALLY_DEFECT": tc.defect, "TALLY_SIMULATE_LOCK": tc.locked,
			} {
				t.Setenv(key, value)
			}
			before := readTree(t, product)
			ledgerBefore := readTree(t, ledger)
			run := launchKept(t, dir, coreBin, "", "--project", product, "--home", home, "--ledger", ledger,
				"--harnesses", templates, "--allow-harness", "--harness", "standin", "--model", "scripted",
				"--intent", "prove the store opens", "--claim", "item-listed")
			var report struct {
				Probes   map[string]any `json:"probes"`
				Commands []struct {
					Label, Stdout, Stderr string
					Exit                  int
				} `json:"commands"`
			}
			if err := json.Unmarshal([]byte(run.home(t, "report.json")), &report); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(report.Probes, standinProbes()) {
				t.Fatalf("sandbox probes:\n got: %v\nwant: %v", report.Probes, standinProbes())
			}
			if len(report.Commands) != len(cases) {
				t.Fatalf("brain attempted %d commands, want %d", len(report.Commands), len(cases))
			}
			for i, result := range report.Commands {
				want := cases[i]
				code := want.Code
				if want.Label == "run claims" || want.Label == "run claims again" {
					code = tc.code
				}
				if want.Label == "run narrower" && tc.locked != "" {
					code = 2
				}
				if result.Label != want.Label || result.Exit != code || !strings.Contains(result.Stdout+result.Stderr, want.Contains) {
					t.Fatalf("%s: exit %d, want %d and %q\nstdout: %s\nstderr: %s", result.Label, result.Exit, code, want.Contains, result.Stdout, result.Stderr)
				}
				if want.Label == "run claims" {
					var doc runDoc
					if json.Unmarshal([]byte(result.Stdout), &doc) != nil || doc.Verdict != tc.verdict || doc.Skipped || doc.Requested.Chain != nil {
						t.Fatalf("real core run: %s", result.Stdout)
					}
				}
				if want.Label == "run claims again" && run.stdout != result.Stdout {
					t.Fatalf("launcher did not return the last core %s byte for byte\nstdout: %s\nstderr: %s", tc.verdict, run.stdout, run.stderr)
				}
			}
			if run.code != tc.code {
				t.Fatalf("launcher exit %d, want %d\nstdout: %s\nstderr: %s", run.code, tc.code, run.stdout, run.stderr)
			}
			if !reflect.DeepEqual(before, readTree(t, product)) || gitStatus(t, product) != "" {
				t.Fatal("the brain changed the project")
			}
			if hits.Load()+sockHits.Load()+abstractHits.Load() != 0 {
				t.Fatal("the brain reached a host socket")
			}
			for _, path := range []string{outside, filepath.Join(run.root, "sandbox", "verify", "escaped"), filepath.Join(run.root, "log", "escaped")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("brain scratch write reached the host: %s", path)
				}
			}
			for _, target := range []string{home, ledger, callerHome, callerLedger, judgments} {
				if _, err := os.Stat(filepath.Join(target, "escaped")); !os.IsNotExist(err) {
					t.Fatalf("the brain wrote outside its sandbox: %s", target)
				}
			}
			for _, path := range []string{credential, otherHome} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != marker {
					t.Fatalf("host file changed: %s: %v", path, err)
				}
			}
			ledgerAfter := readTree(t, ledger)
			for path, data := range ledgerBefore {
				if ledgerAfter[path] != data {
					t.Fatalf("ledger file changed: %s", path)
				}
			}
			rows := instancesDown(t, product, home)
			if len(rows) != 3 {
				t.Fatalf("home has %d runs, want only the three permitted runs", len(rows))
			}
			for _, row := range rows {
				record := readRecord(t, home, row.Run)
				ticket, _ := record["ticket"].(map[string]any)
				if ticket["harness"] != "standin" || ticket["model"] != "scripted" {
					t.Fatalf("brain replaced the pinned ticket: %v", ticket)
				}
			}
			t.Logf("%s: %d commands, %d sandbox probes; host unchanged; only three launcher runs", tc.verdict, len(cases), len(report.Probes))
		})
	}
}

type standinCommand struct {
	Label     string   `json:"label"`
	Args      []string `json:"args"`
	StdinSize int      `json:"stdin_size,omitempty"`
	Code      int      `json:"-"`
	Contains  string   `json:"-"`
}

func standinCommands(core, launcher string) []standinCommand {
	var cases []standinCommand
	add := func(label string, code int, contains string, args ...string) {
		cases = append(cases, standinCommand{Label: label, Args: append([]string{"verilex"}, args...), Code: code, Contains: contains})
	}
	add("index", 0, "item-listed", "index", "--intent", "prove a stored apple is listed")
	add("index changed", 0, "item-listed", "index", "--changed", "bin/tally")
	add("plan", 0, "item-listed", "plan", "--claim", "item-listed")
	add("words", 0, "item-listed", "words")
	add("claims", 0, "item-listed", "claims", "--json")
	add("runs before", 0, "[]", "runs", "--json")
	add("ticket", 0, "forged", "ticket", "--json", "$HOME/ticket.yaml")
	add("check", 0, "check: no drift (3 admitted)", "check")
	for _, cmd := range []string{"cleanup", "new", "onboard", "propose", "admit", "gap", "unknown"} {
		add("refuse "+cmd, 2, "verilex-agent: refused: verilex "+cmd+" is not available", cmd, "item-listed")
	}
	for _, cmd := range []string{"plan", "run"} {
		for _, flag := range [][]string{{"--keep"}, {"--keep=true"}, {"--continue", "other"}, {"--continue=other"}} {
			add(cmd+" "+strings.Join(flag, " "), 2, "not available", append([]string{cmd, "--claim", "item-listed"}, flag...)...)
			if flag[0] == "--keep=true" {
				cases[len(cases)-1].Contains = "unrecognized arguments"
			}
		}
		for _, chain := range []string{"store-open", "", "store-open | item-stored pear | item-listed pear"} {
			add(cmd+" chain "+chain, 2, "--no-chain refuses a chain argument", cmd, "--claim", "item-listed", "--", chain)
		}
	}
	add("oversized stdin", 2, "stdin is too large", "index")
	cases[len(cases)-1].StdinSize = (1 << 20) + 1
	add("run claims", 0, "verilex-claim-run-1", "--project=/missing", "run", "--fresh", "--ticket=$HOME/ticket.yaml", "--claim", "item-listed", "--named", "store-opened", "--project", "/missing")
	add("run narrower", 0, "verilex-claim-run-1", "run", "--fresh", "--claim", "store-opened")
	add("run claims again", 0, "verilex-claim-run-1", "run", "--fresh", "--claim", "item-listed", "--named", "store-opened")
	add("runs after", 0, "run", "runs", "--json")
	// Depending on its readable parents, the real core is absent or masked.
	// Normalize the shell's 126/127, but never a successful execution.
	cases = append(cases, standinCommand{Label: "real core hidden", Args: []string{"sh", "-c", `"$1" --help; rc=$?; [ "$rc" -eq 0 ] && exit 0; exit 1`, "probe", core}, Code: 1})
	cases = append(cases, standinCommand{Label: "core project alias hidden", Args: []string{"sh", "-c", `./standin-core-link --help; rc=$?; [ "$rc" -eq 0 ] && exit 0; exit 1`}, Code: 1})
	cases = append(cases, standinCommand{Label: "launcher help", Args: []string{launcher, "--help"}, Code: 0, Contains: "verilex-agent"})
	return cases
}

func standinProbes() map[string]any {
	want := map[string]any{}
	for _, label := range []string{"read project", "read system", "read brain", "read launcher", "read credential", "write home", "write tmp", "read prompt", "state env removed", "proxy env set", "no controlling terminal", "private namespaces", "start child", "own unix socket", "own tcp socket", "write tmp lookalike outside", "write tmp lookalike state"} {
		want[label] = true
	}
	for _, label := range []string{"write project", "create project file", "mkdir project", "rename project file", "unlink project file", "chmod project file", "symlink in project", "hardlink from project", "write credential", "read other home file", "write system", "write brain executable", "read logs", "write logs", "open terminal", "see host process", "host loopback", "host unix socket", "host abstract socket"} {
		want[label] = false
	}
	for _, label := range []string{"ledger", "verilex home", "caller home", "caller ledger", "judgments"} {
		for _, op := range []string{"read ", "write ", "symlink read "} {
			want[op+label] = false
		}
	}
	for _, target := range []string{"127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "[fc00::1]", "[fe80::1]", "[2001:db8::1]", "[64:ff9b::7f00:1]"} {
		want["proxy "+target] = float64(403)
	}
	for _, target := range []string{"8.8.8.8", "[::ffff:8.8.8.8]", "[2606:4700:4700::1111]"} {
		want["proxy public "+target] = float64(502)
	}
	want["proxy plain http"], want["proxy malformed connect"] = float64(405), float64(400)
	return want
}
