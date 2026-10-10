package sandbox

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Init is the first process inside the sandbox, the verilex-agent sandbox-init subcommand. It
// reads its config from fd 3, checks from the inside that the sandbox holds, opens the egress
// bridges, reports ok or the failure on fd 4, and then runs the brain with argv. It exits with
// the brain's code.
func Init(argv []string, stderr io.Writer) int {
	if len(argv) < 2 || argv[0] != "--" {
		fmt.Fprintln(stderr, "verilex-agent: sandbox-init runs only inside the launcher's sandbox")
		return 2
	}
	argv = argv[1:]
	cfgFile, ready := os.NewFile(3, "sandbox-config"), os.NewFile(4, "sandbox-ready")
	var cfg config
	err := json.NewDecoder(cfgFile).Decode(&cfg)
	cfgFile.Close()
	if err != nil {
		fmt.Fprintf(ready, "fail: no sandbox config: %v\n", err)
		ready.Close()
		return 2
	}
	if reason := holds(cfg); reason != "" {
		fmt.Fprintf(ready, "fail: %s\n", reason)
		ready.Close()
		return 2
	}
	for _, b := range cfg.Bridges {
		ln, err := net.Listen("tcp", b.Listen)
		if err != nil {
			fmt.Fprintf(ready, "fail: egress bridge %s: %v\n", b.Listen, err)
			ready.Close()
			return 2
		}
		go bridge(ln, b.Socket)
	}
	fmt.Fprintln(ready, "ok")
	ready.Close()
	// The launcher's SIGTERM asks this process to end the sandbox before it exits.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, stderr
	if err = cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "verilex-agent: brain: %v\n", err)
		return 127
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err = <-exited:
		// Whatever the brain left running ends with it, even a process that left its session.
		endSandbox()
	case <-stop:
		endSandbox()
		err = <-exited
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal())
		}
		return exit.ExitCode()
	default:
		fmt.Fprintf(stderr, "verilex-agent: brain: %v\n", err)
		return 127
	}
}

// holds checks the sandbox from the inside: the project takes no new file, no hidden path shows
// anything, and the host's loopback is out of reach. It returns why the sandbox does not hold.
func holds(cfg config) string {
	if cfg.ReadOnly != "" {
		probe := filepath.Join(cfg.ReadOnly, ".verilex-agent-check-"+token())
		if f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
			f.Close()
			os.Remove(probe)
			return "the brain could write the project"
		}
	}
	for _, path := range cfg.Hidden {
		if reachable(path) {
			return "the brain could read " + path
		}
	}
	if reason := contained(); reason != "" {
		return reason
	}
	if cfg.Closed != "" {
		if conn, err := net.DialTimeout("tcp", cfg.Closed, 2*time.Second); err == nil {
			conn.Close()
			return "the brain could reach the host's network"
		}
	}
	return ""
}

// reachable reports whether path shows anything: a masked directory is empty and a masked file
// is /dev/null.
func reachable(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	switch {
	case info.IsDir():
		entries, err := os.ReadDir(path)
		return err == nil && len(entries) > 0
	case info.Mode()&os.ModeCharDevice != 0:
		return false
	default:
		f, err := os.Open(path)
		if err != nil {
			return false
		}
		f.Close()
		return true
	}
}

// bridge forwards each connection on ln to the unix socket.
func bridge(ln net.Listener, socket string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			upstream, err := net.Dial("unix", socket)
			if err != nil {
				return
			}
			defer upstream.Close()
			go func() {
				io.Copy(upstream, conn)
				upstream.Close()
			}()
			io.Copy(conn, upstream)
		}()
	}
}

func token() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
