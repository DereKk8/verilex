package e2e_test

import (
	"encoding/json"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// admittedProduct is an admitted tally copy in a git repository with a clean working tree.
func admittedProduct(t *testing.T) (dir, product, ledger string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TALLY_STORES", t.TempDir())
	dir = t.TempDir()
	product = copyProduct(t, dir)
	ledger = filepath.Join(dir, "ledger")
	admit(t, product, ledger)
	if err := commitAll(product); err != nil {
		t.Fatal(err)
	}
	return dir, product, ledger
}

// The brain cannot change the code under test: its sandbox shows the project read-only. A brain
// that tries to fix the product or a word, the tester's P3 and P3b, still gets verilex's red for
// the code as it is, and the project is unchanged.
func TestBrainCannotEditTheProject(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	t.Setenv("TALLY_DEFECT", "hide-lists")
	for _, tc := range []struct{ name, edit string }{
		{"honest", ""},
		{"fixes the product", `python3 -c "p='bin/tally'; s=open(p).read(); open(p,'w').write(s.replace('hide-lists', 'hide-lists-off'))"`},
		{"rewrites a word", `printf '#!/bin/sh\necho ok\n' > .verilex/words/item-listed/run`},
		{"adds a file to a word", `printf 'x\n' > .verilex/words/item-listed/extra.txt`},
		{"moves the product aside", "mv bin/tally bin/tally-off"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "#!/bin/sh\n"
			if tc.edit != "" {
				body += "verilex run --claim item-listed > /dev/null\n(" + tc.edit + ") 2> \"$HOME/edit\"\necho \"edit exit $?\" >> \"$HOME/edit\"\n"
			}
			body += "verilex run --claim item-listed > /dev/null\n"
			brain := writeBrain(t, filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")), body)
			run := launchKept(t, dir, coreBin, brain,
				"--project", product, "--ledger", ledger, "--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
			var doc runDoc
			if run.code != 1 || json.Unmarshal([]byte(run.stdout), &doc) != nil || doc.Verdict != "red" {
				t.Fatalf("exit %d, want verilex's red\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
			}
			if tc.edit != "" && strings.Contains(run.home(t, "edit"), "edit exit 0") {
				t.Fatalf("the edit succeeded inside the sandbox:\n%s", run.home(t, "edit"))
			}
			if status := gitStatus(t, product); status != "" {
				t.Fatalf("the project changed:\n%s", status)
			}
		})
	}
}

// The time budget bounds the whole run, both brain phases, and no child of the brain can hold
// the launcher past it. Without a budget, a background child cannot hold the launcher either.
func TestBudgetBoundsTheRunWhateverTheBrainStarts(t *testing.T) {
	escape := "python3 -c 'import os, time; os.setsid(); time.sleep(8)' &\n"
	for _, tc := range []struct {
		name, body, budget string
		suggest            bool
		exit               int
	}{
		{"setsid child", escape + "sleep 8\n", "300ms", false, 2},
		{"suggest phase", "if [ \"$VERILEX_AGENT_PHASE\" = suggest ]; then sleep 8; fi\nverilex run --named store-opened\n", "300ms", true, 2},
		{"background child, no budget", "sleep 8 &\n" + escape + "verilex run --named store-opened\n", "", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ticket := `{"intent":"prove the store opens","harness":"stub","model":"stub","time_budget":"` + tc.budget + `"}`
			fake := writeFake(t, dir, fakeFiles{ticket: []byte(ticket + "\n"), run: claimRun("green", "r1", []string{"store-opened"}, nil)})
			brain := writeBrain(t, dir, "#!/bin/sh\n"+tc.body)
			args := []string{"--intent", "prove the store opens", "--claim", "store-opened", "--harness", "stub", "--model", "stub"}
			if tc.suggest {
				args = append(args, "--suggest")
			}
			start := time.Now()
			stdout, stderr, code := launch(t, dir, fake, brain, args...)
			if took := time.Since(start); took > 3*time.Second {
				t.Fatalf("launcher took %s\nstderr: %s", took, stderr)
			}
			if code != tc.exit {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, tc.exit, stdout, stderr)
			}
			if tc.exit == 2 && (stdout != "" || !strings.Contains(stderr, "inconclusive: the time budget ended")) {
				t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
			}
		})
	}
}

// The brain cannot reach the real verilex binary or the run's home, so it cannot run verilex
// around the launcher or keep an instance, the tester's P5: the only run in the home is the one
// it asked the launcher for.
func TestBrainCannotReachTheRealVerilex(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("REAL_VERILEX", coreBin)
	t.Setenv("AGENT_HOME", home)
	brain := writeBrain(t, dir, `#!/bin/sh
VERILEX_HOME="$AGENT_HOME" "$REAL_VERILEX" run --keep --claim store-opened > /dev/null 2> "$HOME/bypass"
echo "bypass exit $?" >> "$HOME/bypass"
command -v verilex > "$HOME/which"
verilex run --claim store-opened > /dev/null
`)
	run := launchKept(t, dir, coreBin, brain,
		"--project", product, "--home", home, "--ledger", ledger, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub")
	var doc runDoc
	if run.code != 0 || json.Unmarshal([]byte(run.stdout), &doc) != nil || doc.Verdict != "green" {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
	}
	if bypass := run.home(t, "bypass"); strings.Contains(bypass, "bypass exit 0") {
		t.Fatalf("the brain ran the real verilex:\n%s", bypass)
	}
	which, _ := filepath.EvalSymlinks(strings.TrimSpace(run.home(t, "which")))
	if want, _ := filepath.EvalSymlinks(filepath.Join(run.root, "share", "bin", "verilex")); which == "" || which != want {
		t.Fatalf("the brain's verilex is %q, not the launcher's %q", which, want)
	}
	if rows := instancesDown(t, product, home); len(rows) != 1 || rows[0].Run != doc.Run {
		t.Fatalf("home runs %+v, want only %s", rows, doc.Run)
	}
}

// The tester's F2 forger (P11) read the ledger path from the launcher's command line in /proc and
// copied honest passes under the stamps of a red run, so the rerun skipped to green. In the
// sandbox the brain finds no such path. Even given the paths, it cannot read the ledger or the
// home, run the real verilex, or reach the host's network or sockets, directly or through the
// egress proxy, and what it writes never reaches the real ledger, home, stores or project. Its
// rerun stays verilex's red, and the ledger only gained the passes the launcher's runs recorded.
func TestBrainCannotForgeTheLedgerOrLeaveItsSandbox(t *testing.T) {
	dir, product, ledger := admittedProduct(t)
	t.Setenv("TALLY_DEFECT", "hide-lists")
	home := filepath.Join(t.TempDir(), "home")
	canary, canaryHits := listen(t, "tcp", "127.0.0.1:0")
	sockDir, err := os.MkdirTemp("/tmp", "vxs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	hostSock, sockHits := listen(t, "unix", filepath.Join(sockDir, "host.sock"))
	_, port, _ := net.SplitHostPort(canary)
	// Loopback by address and by name, private, link-local (the cloud metadata address) and
	// carrier-grade NAT: none is public, so the egress proxy refuses each before it dials.
	targets := []string{"127.0.0.1", "localhost", "[::1]", "10.0.0.1", "192.168.1.1", "169.254.169.254", "100.64.0.1"}
	for key, value := range map[string]string{
		"LEDGER": ledger, "AGENT_HOME": home, "PRODUCT": product, "REAL_VERILEX": coreBin,
		"CANARY": port, "HOST_SOCK": hostSock, "PRIVATE_TARGETS": strings.Join(targets, " "),
	} {
		t.Setenv(key, value)
	}
	before := readTree(t, ledger)
	// The [-] keeps each grep from finding its own command line.
	brain := writeBrain(t, dir, `#!/bin/sh
verilex run --claim item-listed > /dev/null
log="$HOME/probe"
try() {
  label=$1; shift
  if "$@" > /dev/null 2>&1; then echo "$label: reached" >> "$log"; else echo "$label: refused" >> "$log"; fi
}
try "ledger path in /proc" sh -c 'cat /proc/*/cmdline | tr "\000" "\n" | grep -q -x -e "[-]-ledger" -e "[-]-home"'
try "ledger path in ps" sh -c 'ps -A -ww -o args= | grep -q -e "[-]-ledger" -e "[-]-home"'
try "read ledger" sh -c 'find "$LEDGER" -type f | grep -q .'
try "read home" sh -c 'find "$AGENT_HOME" -type f | grep -q .'
try "run the real verilex" "$REAL_VERILEX" --help
try "host loopback" python3 -c 'import os, socket; socket.create_connection(("127.0.0.1", int(os.environ["CANARY"])), 2)'
try "host socket" python3 -c 'import os, socket; s = socket.socket(socket.AF_UNIX); s.connect(os.environ["HOST_SOCK"])'
try "egress to the host or a private network" python3 -c '
import os, socket, sys
host, port = os.environ["HTTPS_PROXY"].split("//")[1].rstrip("/").rsplit(":", 1)
for target in os.environ["PRIVATE_TARGETS"].split():
    s = socket.create_connection((host, int(port)), 5)
    s.sendall(("CONNECT %s:%s HTTP/1.1\r\nHost: %s\r\n\r\n" % (target, os.environ["CANARY"], target)).encode())
    if b" 200 " in s.recv(200):
        sys.exit(0)
sys.exit(1)'
for target in "$LEDGER/tally/passes/forged" "$AGENT_HOME/forged" "$TALLY_STORES/forged" "$PRODUCT/forged"; do
  mkdir -p "$target" 2> /dev/null && echo '{"run": "forged"}' > "$target/pass.json"
done
verilex run --claim item-listed > /dev/null
`)
	run := launchKept(t, dir, coreBin, brain,
		"--project", product, "--home", home, "--ledger", ledger, "--intent", "prove a stored apple is listed", "--harness", "stub", "--model", "stub")
	var doc runDoc
	if run.code != 1 || json.Unmarshal([]byte(run.stdout), &doc) != nil || doc.Verdict != "red" || doc.Skipped {
		t.Fatalf("exit %d, want verilex's red\nstdout: %s\nstderr: %s", run.code, run.stdout, run.stderr)
	}
	probe := run.home(t, "probe")
	if strings.Count(probe, ": refused\n") != 8 || strings.Contains(probe, ": reached") {
		t.Fatalf("the brain left its sandbox:\n%s", probe)
	}
	if n := canaryHits.Load() + sockHits.Load(); n != 0 {
		t.Fatalf("the host's loopback or socket took %d connections", n)
	}
	for _, path := range []string{filepath.Join(ledger, "tally", "passes", "forged"), filepath.Join(home, "forged"), filepath.Join(os.Getenv("TALLY_STORES"), "forged")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("the brain wrote %s", path)
		}
	}
	if status := gitStatus(t, product); status != "" {
		t.Fatalf("the project changed:\n%s", status)
	}
	launched := map[string]bool{}
	for _, row := range instancesDown(t, product, home) {
		launched[row.Run] = true
	}
	if len(launched) != 2 || !launched[doc.Run] {
		t.Fatalf("home runs %v, want the brain's two runs ending in %s", launched, doc.Run)
	}
	after := readTree(t, ledger)
	for path, data := range before {
		if after[path] != data {
			t.Fatalf("the ledger lost or changed %s", path)
		}
	}
	for path, data := range after {
		if _, ok := before[path]; ok || !strings.Contains(path, "/passes/") {
			continue
		}
		var pass struct{ Run string }
		if json.Unmarshal([]byte(data), &pass) != nil || !launched[pass.Run] {
			t.Fatalf("pass %s was not recorded by a run the launcher made:\n%s", path, data)
		}
	}
	egress, err := os.ReadFile(filepath.Join(run.root, "log", "egress.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if !strings.Contains(string(egress), "refused CONNECT "+target+":"+port+": ") {
			t.Fatalf("the egress log does not record the refusal of %s:\n%s", target, egress)
		}
	}
}

// A person starts the launcher in a terminal. The brain has no controlling terminal and cannot
// open the terminal's device by its path, so it can neither type into the user's shell nor print
// into the user's terminal, the tester's M2.
func TestBrainCannotReachTheTerminal(t *testing.T) {
	dir := t.TempDir()
	fake := writeFake(t, dir, fakeFiles{ticket: ticketJSON("stub", "stub"), run: claimRun("green", "r1", []string{"store-opened"}, nil)})
	brain := writeBrain(t, dir, `#!/bin/sh
for path in /dev/tty "$TTY_PATH"; do
  if printf 'TTY-MARKER\n' 2> /dev/null > "$path"; then echo "$path: written"; else echo "$path: refused"; fi
done > "$HOME/tty"
verilex run --named store-opened > /dev/null
`)
	argv, err := launcherArgs(dir, fake, brain, "--intent", "prove the store opens", "--harness", "stub", "--model", "stub", "--keep-work")
	if err != nil {
		t.Fatal(err)
	}
	// The helper starts the launcher on a new terminal, the way a shell would, and prints all the
	// terminal showed. TTY_PATH is that terminal's device.
	helper := filepath.Join(dir, "terminal.py")
	if err = os.WriteFile(helper, []byte(terminalHelper), 0o600); err != nil {
		t.Fatal(err)
	}
	tmp := runTemp(t)
	cmd := exec.Command("python3", append([]string{"-I", helper, agentBin}, argv...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TMPDIR="+tmp)
	shown, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v\nterminal: %s", err, shown)
	}
	if strings.Contains(string(shown), "TTY-MARKER") {
		t.Fatalf("the brain wrote into the terminal:\n%s", shown)
	}
	report := kept{stdout: string(shown), root: runRoot(t, tmp)}.home(t, "tty")
	if strings.Count(report, ": refused") != 2 || !strings.Contains(report, "/dev/tty: refused") {
		t.Fatalf("the brain reached a terminal:\n%s", report)
	}
}

// terminalHelper runs its arguments on a new pseudo-terminal, prints what the terminal showed,
// and exits with the command's code.
const terminalHelper = `import os, pty, sys

pid, fd = pty.fork()
if pid == 0:
    os.environ["TTY_PATH"] = os.ttyname(0)
    os.execv(sys.argv[1], sys.argv[1:])
shown = b""
while True:
    try:
        chunk = os.read(fd, 65536)
    except OSError:
        break
    if not chunk:
        break
    shown += chunk
_, status = os.waitpid(pid, 0)
sys.stdout.buffer.write(shown)
sys.exit(os.waitstatus_to_exitcode(status))
`

// listen opens a listener on the host that counts the connections it accepts.
func listen(t *testing.T, network, address string) (string, *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen(network, address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var hits atomic.Int64
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			conn.Close()
		}
	}()
	return ln.Addr().String(), &hits
}

// readTree maps the slash path of each regular file under dir to its content.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

type homeRun struct{ Run, Cleanup string }

// instancesDown is what verilex runs lists in home, after it checked that no run there left its
// instance up or its tally store behind.
func instancesDown(t *testing.T, product, home string) []homeRun {
	t.Helper()
	rows := homeRuns(t, product, home)
	for _, row := range rows {
		if row.Cleanup == "kept" || row.Cleanup == "pending" {
			t.Fatalf("run %s cleanup=%s: an instance is still up", row.Run, row.Cleanup)
		}
		if _, err := os.Stat(filepath.Join(os.Getenv("TALLY_STORES"), "tally-"+row.Run)); !os.IsNotExist(err) {
			t.Fatalf("the store of run %s is still there", row.Run)
		}
	}
	return rows
}

// homeRuns is what verilex runs lists in home.
func homeRuns(t *testing.T, product, home string) []homeRun {
	t.Helper()
	cmd := exec.Command(coreBin, "--project", product, "runs", "--json")
	cmd.Env = append(os.Environ(), "VERILEX_HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var rows []homeRun
	if err = json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("runs %s: %v", out, err)
	}
	return rows
}
