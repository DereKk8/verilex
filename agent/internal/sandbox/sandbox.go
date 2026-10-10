// Package sandbox runs the brain where it cannot reach what decides a verdict. The brain may read
// the project and its own tools and write only its own directory. The verilex home, the ledger,
// every verilex binary and the rest of the user's home are not there, and the network is reachable
// only through an egress proxy that refuses the host and private networks. Linux uses bubblewrap
// (bwrap), macOS uses sandbox-exec. Where neither can start, Start returns *Unavailable and the
// brain does not run.
package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/DereKk8/verilex/agent/internal/egress"
)

// Spec is one brain phase to run in the sandbox.
type Spec struct {
	// Argv is the brain command. A bare argv[0] is looked up on the PATH in Env.
	Argv []string
	// Env is the brain's environment. Start sets HOME, TMPDIR and the proxy variables in it.
	Env []string
	// Project is the brain's working directory. The brain may read it and never write it.
	Project string
	// Brain is the only directory the brain may write: its home is Brain/home and its temporary
	// directory Brain/tmp.
	Brain string
	// State is a launcher-owned directory for the sandbox's own files. The brain cannot write it.
	State string
	// Read lists more files and directories the brain may read. Every directory on the PATH in
	// Env is readable too, so the brain finds its tools.
	Read []string
	// HomeFiles maps a path relative to the brain's home to a host file the brain may read there,
	// such as a harness's credentials. The host file stays read-only.
	HomeFiles map[string]string
	// Hide lists paths the brain must not reach even inside a readable directory.
	Hide []string
	// Sockets lists unix sockets the brain may connect to.
	Sockets []string
	Stdin   io.Reader
	Stdout  *os.File
	Stderr  *os.File
	// EgressLog receives one line per network request the brain makes.
	EgressLog io.Writer
}

// Unavailable means the sandbox could not be established, so the brain did not run.
type Unavailable struct{ Reason string }

func (e *Unavailable) Error() string { return e.Reason }

func unavailable(format string, args ...any) error {
	return &Unavailable{Reason: fmt.Sprintf(format, args...)}
}

// Process is a brain running in the sandbox.
type Process struct {
	cmd    *exec.Cmd
	egress *egress.Proxy
	// exited is closed once the sandbox's first process has exited, and err is then its error.
	exited chan struct{}
	err    error
}

// Wait waits for the brain to exit, then ends everything it started and closes its network.
func (p *Process) Wait() error {
	<-p.exited
	p.killGroup()
	p.egress.Close()
	return p.err
}

// Kill ends the brain and every process it started, including one that left its session.
func (p *Process) Kill() {
	p.stop()
	p.killGroup()
}

// killGroup ends the sandbox's first process and every process still in its session.
func (p *Process) killGroup() {
	syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
}

// Start runs spec's brain in the sandbox. It returns once the sandbox has checked from the inside
// that it holds, or *Unavailable when it could not be established or did not hold.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	v, err := newView(spec, self)
	if err != nil {
		return nil, err
	}
	network, address := egressEndpoint(spec)
	eg, err := egress.Listen(network, address, spec.EgressLog)
	if err != nil {
		return nil, fmt.Errorf("egress proxy: %v", err)
	}
	go eg.Serve()
	proc, err := start(ctx, spec, v, self, eg)
	if err != nil {
		eg.Close()
		return nil, err
	}
	return proc, nil
}

func start(ctx context.Context, spec Spec, v *view, self string, eg *egress.Proxy) (*Process, error) {
	// A port on the host's loopback the brain must not reach: the check inside dials it.
	canary, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer canary.Close()
	cmd, bridges, err := command(spec, v, self, eg.Addr())
	if err != nil {
		return nil, err
	}
	cfg := config{
		Bridges:  bridges,
		ReadOnly: spec.Project,
		Hidden:   v.checks,
		Closed:   canary.Addr().String(),
	}
	cfgRead, cfgWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		cfgRead.Close()
		cfgWrite.Close()
		return nil, err
	}
	defer readyRead.Close()
	cmd.ExtraFiles = []*os.File{cfgRead, readyWrite}
	cmd.Stdin = spec.Stdin
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	// A session of its own: the brain has no controlling terminal, so it cannot write into the
	// terminal the launcher runs in. The session is also the process group killGroup ends.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	cfgRead.Close()
	readyWrite.Close()
	if err != nil {
		cfgWrite.Close()
		return nil, unavailable("%s did not start: %v", filepath.Base(cmd.Path), err)
	}
	proc := &Process{cmd: cmd, egress: eg, exited: make(chan struct{})}
	go func() {
		proc.err = cmd.Wait()
		close(proc.exited)
	}()
	json.NewEncoder(cfgWrite).Encode(cfg)
	cfgWrite.Close()
	line := make(chan string, 1)
	go func() {
		text, _ := bufio.NewReader(readyRead).ReadString('\n')
		line <- strings.TrimSpace(text)
	}()
	select {
	case text := <-line:
		if text == "ok" {
			return proc, nil
		}
		proc.Kill()
		<-proc.exited
		if reason, ok := strings.CutPrefix(text, "fail: "); ok {
			return nil, unavailable("the sandbox did not hold: %s", reason)
		}
		return nil, unavailable("%s could not start the sandbox: %s", filepath.Base(cmd.Path), firstLine(spec.Stderr))
	case <-ctx.Done():
		proc.Kill()
		<-proc.exited
		return nil, ctx.Err()
	}
}

// config is what the launcher hands the helper inside the sandbox. It travels on a pipe, so no
// path in it shows on a command line the brain can read.
type config struct {
	Bridges  []Bridge `json:"bridges"`
	ReadOnly string   `json:"read_only"`
	Hidden   []string `json:"hidden"`
	Closed   string   `json:"closed"`
}

// Bridge forwards TCP connections on Listen, inside the sandbox, to the unix socket Socket.
type Bridge struct {
	Listen string `json:"listen"`
	Socket string `json:"socket"`
}

// view is what the brain may reach, as host paths.
type view struct {
	home, tmp string
	// read holds real paths the brain may read. path holds the PATH directories as written,
	// which may go through symlinks.
	read, path []string
	// hide holds real paths the brain must not reach; checks is every form of them the helper
	// confirms it cannot reach.
	hide, checks []string
}

func newView(spec Spec, self string) (*view, error) {
	home, tmp := filepath.Join(spec.Brain, "home"), filepath.Join(spec.Brain, "tmp")
	for _, dir := range []string{home, tmp, spec.State} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	v := &view{home: realOr(home), tmp: realOr(tmp)}
	for _, path := range spec.Hide {
		if path == "" {
			continue
		}
		v.checks = append(v.checks, filepath.Clean(path))
		if real, err := filepath.EvalSymlinks(path); err == nil {
			v.hide = append(v.hide, real)
			v.checks = append(v.checks, real)
		}
	}
	userHome, _ := os.UserHomeDir()
	if real, err := filepath.EvalSymlinks(userHome); err == nil {
		userHome = real
	}
	for _, dir := range filepath.SplitList(lookupEnv(spec.Env, "PATH")) {
		if !filepath.IsAbs(dir) || tooBroad(dir, userHome, spec.Brain) || v.hidden(realOr(dir)) {
			continue
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		dir = filepath.Clean(dir)
		v.path = append(v.path, dir)
		// A verilex on the brain's PATH outside the launcher's own files is the real binary.
		if target := filepath.Join(dir, "verilex"); exists(target) && !withinAny(realOr(dir), realAll(spec.Read)) {
			v.hide = append(v.hide, realOr(target))
			v.checks = append(v.checks, target)
		}
	}
	read := append([]string{self}, spec.Read...)
	read = append(read, gitDirs(spec.Project)...)
	for _, target := range spec.HomeFiles {
		read = append(read, target)
	}
	for _, path := range read {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			continue
		}
		if !v.hidden(real) {
			v.read = append(v.read, real)
		}
	}
	for rel, target := range spec.HomeFiles {
		if !filepath.IsLocal(rel) || !exists(target) {
			continue
		}
		link := filepath.Join(v.home, rel)
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			return nil, err
		}
		if err := os.Symlink(realOr(target), link); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	return v, nil
}

// hidden reports whether path is a hidden path or inside one.
func (v *view) hidden(path string) bool {
	for _, h := range v.hide {
		if within(path, h) {
			return true
		}
	}
	return false
}

// tooBroad refuses a PATH entry that would show the whole user home or the brain's own directory.
func tooBroad(dir, userHome, brain string) bool {
	dir = realOr(dir)
	return dir == "/" || (userHome != "" && within(userHome, dir)) || within(realOr(brain), dir)
}

// gitDirs are the repository directories git needs when the project is a worktree whose git
// directory lives elsewhere.
func gitDirs(project string) []string {
	var out []string
	for _, flag := range []string{"--git-dir", "--git-common-dir"} {
		cmd := exec.Command("git", "rev-parse", "--path-format=absolute", flag)
		cmd.Dir = project
		if text, err := cmd.Output(); err == nil {
			if dir := strings.TrimSpace(string(text)); dir != "" && !within(realOr(dir), realOr(project)) {
				out = append(out, dir)
			}
		}
	}
	return out
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && filepath.IsLocal(rel)
}

func withinAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if within(path, dir) {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func realOr(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

func realAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = realOr(path)
	}
	return out
}

func lookupEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], key+"="); ok {
			return value
		}
	}
	return ""
}

// brainEnv is env without the variables the sandbox sets, plus set.
func brainEnv(env []string, set map[string]string) []string {
	drop := map[string]bool{
		"HOME": true, "TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_CACHE_HOME": true,
		"XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true, "NO_PROXY": true, "no_proxy": true,
	}
	for key := range set {
		drop[key] = true
	}
	var out []string
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if !drop[key] {
			out = append(out, entry)
		}
	}
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		out = append(out, key+"="+set[key])
	}
	return out
}

// proxyEnv points every common client at the egress proxy.
func proxyEnv(url string) map[string]string {
	return map[string]string{
		"HTTPS_PROXY": url, "https_proxy": url, "HTTP_PROXY": url, "http_proxy": url,
		"ALL_PROXY": url, "all_proxy": url, "NODE_USE_ENV_PROXY": "1",
	}
}

func firstLine(f *os.File) string {
	if f == nil {
		return "no detail"
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "no detail"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "no detail"
}
