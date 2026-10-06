// Package proxy runs the real verilex binary for the brain and keeps the bytes verilex printed.
// The brain can only ask for a command. It cannot supply the verdict the launcher returns.
package proxy

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxStdin = 1 << 20

// Proxy is the launcher side of the socket the brain's verilex command calls.
type Proxy struct {
	Verilex string
	Project string
	Home    string
	Ledger  string
	Env     []string

	ln   net.Listener
	mu   sync.Mutex
	cmds []*exec.Cmd
	run  capture
	plan capture
}

type capture struct {
	stdout []byte
	exit   int
	ok     bool
}

type request struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin"`
}

type response struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Exit   int    `json:"exit"`
}

// Listen opens a short socket path. Close removes it.
func Listen(dir string) (*Proxy, error) {
	sock := filepath.Join(dir, fmt.Sprintf("vxag-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, err
	}
	return &Proxy{ln: ln}, nil
}

// Socket is the path a relay dials.
func (p *Proxy) Socket() string { return p.ln.Addr().String() }

// Serve accepts until Close. It returns when the listener closes.
func (p *Proxy) Serve() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			p.handle(conn)
		}()
	}
}

// LastRun is the last verilex run the brain asked for.
func (p *Proxy) LastRun() (stdout []byte, exit int, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.run.stdout...), p.run.exit, p.run.ok
}

// LastPlan is the last verilex plan the brain asked for.
func (p *Proxy) LastPlan() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.plan.stdout...)
}

// Close stops the listener and kills any verilex process the proxy still holds.
func (p *Proxy) Close() error {
	sock := p.Socket()
	err := p.ln.Close()
	p.mu.Lock()
	cmds := append([]*exec.Cmd(nil), p.cmds...)
	p.mu.Unlock()
	for _, cmd := range cmds {
		if cmd.Process == nil {
			continue
		}
		killTree(cmd.Process.Pid)
	}
	os.Remove(sock)
	return err
}

func (p *Proxy) handle(conn net.Conn) {
	var req request
	if err := readJSON(conn, &req); err != nil {
		writeJSON(conn, response{Stderr: "verilex-agent: relay read failed\n", Exit: 2})
		return
	}
	if len(req.Stdin) > maxStdin {
		writeJSON(conn, response{Stderr: "verilex-agent: refused: stdin is too large\n", Exit: 2})
		return
	}
	args := pinArgs(req.Args, p.Project)
	stdout, stderr, code := p.exec(args, req.Stdin)
	sub := subcommand(args)
	p.mu.Lock()
	switch sub {
	case "run":
		p.run = capture{stdout: append([]byte(nil), stdout...), exit: code, ok: true}
	case "plan":
		p.plan = capture{stdout: append([]byte(nil), stdout...), exit: code, ok: true}
	}
	p.mu.Unlock()
	writeJSON(conn, response{Stdout: string(stdout), Stderr: string(stderr), Exit: code})
}

func (p *Proxy) exec(args []string, stdin string) ([]byte, []byte, int) {
	cmd := exec.Command(p.Verilex, args...)
	cmd.Dir = p.Project
	cmd.Env = p.childEnv()
	cmd.Stdin = strings.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	p.mu.Lock()
	p.cmds = append(p.cmds, cmd)
	p.mu.Unlock()
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), exitCode(err)
}

func (p *Proxy) childEnv() []string {
	var env []string
	for _, entry := range p.Env {
		if strings.HasPrefix(entry, "VERILEX_HOME=") || strings.HasPrefix(entry, "VERILEX_LEDGER=") || strings.HasPrefix(entry, "TALLY_STORES=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "VERILEX_HOME="+p.Home)
	env = append(env, "TALLY_STORES="+filepath.Join(p.Home, "stores"))
	if p.Ledger != "" {
		env = append(env, "VERILEX_LEDGER="+p.Ledger)
	}
	return env
}

// Relay dials socket, runs one verilex command, and copies its output to the caller.
func Relay(socket string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	body, err := io.ReadAll(io.LimitReader(stdin, maxStdin+1))
	if err != nil {
		fmt.Fprintf(stderr, "verilex-agent: relay: %v\n", err)
		return 2
	}
	if len(body) > maxStdin {
		fmt.Fprintln(stderr, "verilex-agent: refused: stdin is too large")
		return 2
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		fmt.Fprintf(stderr, "verilex-agent: relay: %v\n", err)
		return 2
	}
	defer conn.Close()
	if err = writeJSON(conn, request{Args: args, Stdin: string(body)}); err != nil {
		fmt.Fprintf(stderr, "verilex-agent: relay: %v\n", err)
		return 2
	}
	var resp response
	if err = readJSON(conn, &resp); err != nil {
		fmt.Fprintf(stderr, "verilex-agent: relay: %v\n", err)
		return 2
	}
	io.WriteString(stdout, resp.Stdout)
	io.WriteString(stderr, resp.Stderr)
	return resp.Exit
}

func pinArgs(args []string, project string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--project" && i+1 < len(args) {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--project=") {
			continue
		}
		out = append(out, args[i])
	}
	out = append([]string{"--project", project}, out...)
	if sub := subcommand(out); (sub == "run" || sub == "plan") && !hasFlag(out, "--json") {
		out = append(out, "--json")
	}
	return out
}

func subcommand(args []string) string {
	values := map[string]bool{
		"--project": true, "--continue": true, "--ticket": true, "--intent": true,
		"--changed": true, "--implements": true, "--verdict": true, "--same-as": true,
		"--claim": true, "--claims": true, "--named": true, "--diff": true, "--from": true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			name, _, cut := strings.Cut(arg, "=")
			if !cut && values[name] {
				i++
			}
			continue
		}
		return arg
	}
	return ""
}

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func writeJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(data)))
	if _, err = w.Write(size[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func readJSON(r io.Reader, value any) error {
	var size [4]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n > 32<<20 {
		return errors.New("message too large")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 2
}

func killTree(pid int) {
	killChildren(pid)
	syscall.Kill(-pid, syscall.SIGKILL)
	syscall.Kill(pid, syscall.SIGKILL)
}

func killChildren(pid int) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/task/" + strconv.Itoa(pid) + "/children")
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(data)) {
		n, err := strconv.Atoi(field)
		if err == nil && n > 0 {
			killTree(n)
		}
	}
}
