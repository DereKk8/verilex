//go:build !linux && !darwin

package sandbox

import (
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
)

func egressEndpoint(spec Spec) (string, string) {
	return "unix", filepath.Join(spec.State, "egress.sock")
}

func command(spec Spec, v *view, self string, egressAddr net.Addr) (*exec.Cmd, []Bridge, error) {
	return nil, nil, unavailable("verilex-agent has no sandbox for %s", runtime.GOOS)
}

func (p *Process) stop() {}
