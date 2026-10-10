//go:build linux

package sandbox

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// bridgeAddr is where the brain's HTTP proxy listens, inside the sandbox's own network namespace.
// The helper forwards it to the launcher's egress proxy.
const bridgeAddr = "127.0.0.1:3128"

// systemDirs are the host directories every brain may read: the OS, its libraries and its config.
var systemDirs = []string{"/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc", "/opt", "/nix/store", "/sys"}

func egressEndpoint(spec Spec) (string, string) {
	return "unix", filepath.Join(spec.State, "egress.sock")
}

// command is bwrap with new user, mount, pid, network, ipc, uts and cgroup namespaces. The mount
// namespace holds only what the brain may read, read-only, plus its own directory; the network
// namespace has only a loopback, where the helper bridges to the egress proxy. The brain gets its
// own session, so it has no terminal to type into, and the whole sandbox dies with bwrap.
func command(spec Spec, v *view, self string, egressAddr net.Addr) (*exec.Cmd, []Bridge, error) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, nil, unavailable("bubblewrap (bwrap) is not installed")
	}
	egressSock := egressAddr.String()
	var binds []bind
	for _, dir := range systemDirs {
		info, err := os.Lstat(dir)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(dir); err == nil {
				binds = append(binds, bind{op: "--symlink", src: target, dst: dir})
			}
		} else if info.IsDir() {
			binds = append(binds, bind{op: "--ro-bind", src: dir, dst: dir})
		}
	}
	binds = append(binds,
		bind{op: "--bind", src: v.tmp, dst: "/tmp"},
		bind{op: "--bind", src: v.home, dst: v.home},
		bind{op: "--ro-bind", src: spec.Project, dst: spec.Project},
	)
	for _, path := range v.read {
		binds = append(binds, bind{op: "--ro-bind", src: path, dst: path})
	}
	for _, dir := range v.path {
		binds = append(binds, bind{op: "--ro-bind", src: dir, dst: dir})
	}
	for _, sock := range append(slices.Clone(spec.Sockets), egressSock) {
		binds = append(binds, bind{op: "--ro-bind", src: sock, dst: sock})
	}
	binds = dedupe(binds)
	// Parents mount before their children, so a later mount is never hidden by an earlier one.
	slices.SortStableFunc(binds, func(a, b bind) int { return depth(a.dst) - depth(b.dst) })
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--proc", "/proc", "--dev", "/dev"}
	for _, b := range binds {
		args = append(args, b.op, b.src, b.dst)
	}
	// Masks go last: a hidden path inside a readable directory shows as an empty read-only
	// directory or as /dev/null.
	for _, mask := range masks(v, binds) {
		if info, err := os.Stat(mask.host); err == nil && info.IsDir() {
			args = append(args, "--tmpfs", mask.dst, "--remount-ro", mask.dst)
		} else {
			args = append(args, "--ro-bind", "/dev/null", mask.dst)
		}
	}
	args = append(args, "--remount-ro", "/", "--chdir", spec.Project, "--", self, "sandbox-init", "--")
	args = append(args, spec.Argv...)
	cmd := exec.Command(bwrap, args...)
	set := proxyEnv("http://" + bridgeAddr)
	set["HOME"] = v.home
	set["TMPDIR"] = "/tmp"
	cmd.Env = brainEnv(spec.Env, set)
	return cmd, []Bridge{{Listen: bridgeAddr, Socket: egressSock}}, nil
}

type bind struct{ op, src, dst string }

func depth(path string) int { return strings.Count(filepath.Clean(path), "/") }

// dedupe drops a bind whose destination an earlier bind already holds.
func dedupe(binds []bind) []bind {
	seen := map[string]bool{}
	var out []bind
	for _, b := range binds {
		dst := filepath.Clean(b.dst)
		if seen[dst] {
			continue
		}
		seen[dst] = true
		out = append(out, b)
	}
	return out
}

type mask struct{ host, dst string }

// masks places every hidden path that a read-only bind would show at its place in the sandbox.
func masks(v *view, binds []bind) []mask {
	seen := map[string]bool{}
	var out []mask
	for _, hidden := range v.hide {
		for _, b := range binds {
			if b.op != "--ro-bind" {
				continue
			}
			src := realOr(b.src)
			if !within(hidden, src) {
				continue
			}
			rel, _ := filepath.Rel(src, hidden)
			dst := filepath.Join(b.dst, rel)
			if !seen[dst] {
				seen[dst] = true
				out = append(out, mask{host: hidden, dst: dst})
			}
		}
	}
	return out
}

// stop has nothing to add on Linux: killGroup ends bwrap, and the whole sandbox, every session in
// its pid namespace included, dies with it.
func (p *Process) stop() {}
