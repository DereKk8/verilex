//go:build darwin

package sandbox

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func egressEndpoint(spec Spec) (string, string) {
	return "tcp", "127.0.0.1:0"
}

// command is sandbox-exec with a profile that denies by default. The brain may read the system
// and what the view lists, write only its own directory, connect only to the egress proxy's port
// and the launcher's sockets, and signal or inspect only processes in its own sandbox.
func command(spec Spec, v *view, self string, egressAddr net.Addr) (*exec.Cmd, []Bridge, error) {
	sandboxExec, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, nil, unavailable("sandbox-exec is not available")
	}
	_, port, err := net.SplitHostPort(egressAddr.String())
	if err != nil {
		return nil, nil, err
	}
	profile, err := seatbelt(spec, v, port)
	if err != nil {
		return nil, nil, unavailable("%v", err)
	}
	path := filepath.Join(spec.State, "sandbox.sb")
	if err = os.WriteFile(path, []byte(profile), 0o600); err != nil {
		return nil, nil, err
	}
	args := append([]string{"-f", path, self, "sandbox-init", "--"}, spec.Argv...)
	cmd := exec.Command(sandboxExec, args...)
	cmd.Dir = spec.Project
	set := proxyEnv("http://" + egressAddr.String())
	set["HOME"] = v.home
	set["TMPDIR"] = v.tmp
	cmd.Env = brainEnv(spec.Env, set)
	return cmd, nil, nil
}

// seatbelt is the profile. Seatbelt applies the last rule that matches, so the order is: read
// everything, deny the user and temporary areas, allow what the view lists, then deny reading,
// writing and running what must stay hidden. Running needs its own rule: exec does not read. The
// terminal devices are denied last.
func seatbelt(spec Spec, v *view, port string) (string, error) {
	var b strings.Builder
	b.WriteString(base)
	userHome, _ := os.UserHomeDir()
	denied := []string{"/Users", "/Volumes", "/private/tmp", "/private/var/tmp", "/private/var/folders", "/private/var/root"}
	if userHome != "" {
		denied = append(denied, userHome, realOr(userHome))
	}
	// The data volume holds the same files under a second name.
	for _, path := range denied {
		if strings.HasPrefix(path, "/Users") || strings.HasPrefix(path, "/private") {
			denied = append(denied, "/System/Volumes/Data"+path)
		}
	}
	b.WriteString("(allow file-read*)\n")
	if err := rule(&b, "deny file-read*", denied, nil); err != nil {
		return "", err
	}
	readable := append([]string{spec.Project, realOr(spec.Brain)}, v.read...)
	readable = append(readable, v.path...)
	for _, dir := range v.path {
		readable = append(readable, realOr(dir))
	}
	for _, sock := range spec.Sockets {
		readable = append(readable, sock, realOr(sock))
	}
	if err := rule(&b, "allow file-read*", readable, nil); err != nil {
		return "", err
	}
	if err := rule(&b, "deny file-read* file-write* process-exec", append(v.hide, v.checks...), nil); err != nil {
		return "", err
	}
	b.WriteString("(allow file-read-metadata (vnode-type DIRECTORY))\n")
	if err := rule(&b, "allow file-write*", []string{realOr(spec.Brain)}, nil); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "(allow network-outbound (remote ip \"localhost:%s\"))\n", port)
	b.WriteString("(allow system-socket (socket-domain AF_UNIX))\n")
	brain := realOr(spec.Brain)
	if err := rule(&b, "allow network-bind", []string{brain}, unixLocal); err != nil {
		return "", err
	}
	sockets := []string{brain}
	for _, sock := range spec.Sockets {
		sockets = append(sockets, realOr(sock))
	}
	if err := rule(&b, "allow network-outbound", sockets, unixRemote); err != nil {
		return "", err
	}
	// No terminal, and last so no rule above allows one: the brain can neither read the user's
	// keystrokes nor write into a terminal, its own session's or another's.
	b.WriteString("(deny file-read* file-write* file-ioctl (regex #\"^/dev/tty\") (regex #\"^/dev/pty\") (literal \"/dev/ptmx\"))\n")
	return b.String(), nil
}

func unixLocal(filter string) string  { return "(local unix-socket " + filter + ")" }
func unixRemote(filter string) string { return "(remote unix-socket " + filter + ")" }

// rule writes one (action filters...) form: a subpath filter for a directory and a literal one
// for anything else. wrap, when set, nests each filter, as unix-socket filters need.
func rule(b *strings.Builder, action string, paths []string, wrap func(string) string) error {
	var filters []string
	seen := map[string]bool{}
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		quoted, err := quote(path)
		if err != nil {
			return err
		}
		var filter string
		switch info, err := os.Stat(path); {
		case err == nil && info.IsDir():
			filter = "(subpath " + quoted + ")"
		case wrap != nil:
			filter = "(path-literal " + quoted + ")"
		default:
			filter = "(literal " + quoted + ")"
		}
		if wrap != nil {
			filter = wrap(filter)
		}
		filters = append(filters, filter)
	}
	if len(filters) == 0 {
		return nil
	}
	fmt.Fprintf(b, "(%s\n  %s)\n", action, strings.Join(filters, "\n  "))
	return nil
}

// quote writes path as a profile string. A path with a control character is refused rather than
// guessed at.
func quote(path string) (string, error) {
	for _, r := range path {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("path %q has a control character, which the sandbox profile cannot hold", path)
		}
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`, nil
}

// base allows what any process needs and nothing that reaches outside the sandbox: no
// LaunchServices, Apple Events or launchd jobs, which would start a process outside it, and no
// terminal device. It follows the Chromium-derived policies of Codex and Claude Code's sandbox
// runtime, without their pseudo-terminal rules.
const base = `(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow signal (target same-sandbox))
(allow process-info* (target same-sandbox))
(allow sysctl-read
  (sysctl-name "hw.activecpu")
  (sysctl-name "hw.busfrequency_compat")
  (sysctl-name "hw.byteorder")
  (sysctl-name "hw.cacheconfig")
  (sysctl-name "hw.cachelinesize_compat")
  (sysctl-name "hw.cpufamily")
  (sysctl-name "hw.cpufrequency")
  (sysctl-name "hw.cpufrequency_compat")
  (sysctl-name "hw.cputype")
  (sysctl-name "hw.l1dcachesize_compat")
  (sysctl-name "hw.l1icachesize_compat")
  (sysctl-name "hw.l2cachesize_compat")
  (sysctl-name "hw.l3cachesize_compat")
  (sysctl-name "hw.logicalcpu")
  (sysctl-name "hw.logicalcpu_max")
  (sysctl-name "hw.machine")
  (sysctl-name "hw.memsize")
  (sysctl-name "hw.model")
  (sysctl-name "hw.ncpu")
  (sysctl-name "hw.nperflevels")
  (sysctl-name "hw.packages")
  (sysctl-name "hw.pagesize")
  (sysctl-name "hw.pagesize_compat")
  (sysctl-name "hw.physicalcpu")
  (sysctl-name "hw.physicalcpu_max")
  (sysctl-name "hw.tbfrequency_compat")
  (sysctl-name "hw.vectorunit")
  (sysctl-name "kern.argmax")
  (sysctl-name "kern.hostname")
  (sysctl-name "kern.maxfiles")
  (sysctl-name "kern.maxfilesperproc")
  (sysctl-name "kern.maxproc")
  (sysctl-name "kern.ngroups")
  (sysctl-name "kern.osproductversion")
  (sysctl-name "kern.osrelease")
  (sysctl-name "kern.ostype")
  (sysctl-name "kern.osvariant_status")
  (sysctl-name "kern.osversion")
  (sysctl-name "kern.secure_kernel")
  (sysctl-name "kern.sysv.semmns")
  (sysctl-name "kern.usrstack64")
  (sysctl-name "kern.version")
  (sysctl-name "machdep.cpu.brand_string")
  (sysctl-name "machdep.ptrauth_enabled")
  (sysctl-name "sysctl.proc_cputype")
  (sysctl-name "vm.loadavg")
  (sysctl-name-prefix "hw.optional.")
  (sysctl-name-prefix "hw.perflevel")
  (sysctl-name-prefix "kern.proc.pgrp.")
  (sysctl-name-prefix "kern.proc.pid.")
  (sysctl-name-prefix "machdep.cpu.")
  (sysctl-name-prefix "net.routetable."))
(allow sysctl-write (sysctl-name "kern.grade_cputype"))
(allow mach-lookup
  (global-name "com.apple.bsd.dirhelper")
  (global-name "com.apple.diagnosticd")
  (global-name "com.apple.logd")
  (global-name "com.apple.ocspd")
  (global-name "com.apple.PowerManagement.control")
  (global-name "com.apple.SecurityServer")
  (global-name "com.apple.securityd.xpc")
  (global-name "com.apple.SystemConfiguration.configd")
  (global-name "com.apple.SystemConfiguration.DNSConfiguration")
  (global-name "com.apple.system.logger")
  (global-name "com.apple.system.notification_center")
  (global-name "com.apple.system.opendirectoryd.libinfo")
  (global-name "com.apple.system.opendirectoryd.membership")
  (global-name "com.apple.trustd")
  (global-name "com.apple.trustd.agent"))
(allow ipc-posix-sem)
(allow ipc-posix-shm)
(allow iokit-open (iokit-registry-entry-class "RootDomainUserClient"))
(allow system-socket (require-all (socket-domain AF_SYSTEM) (socket-protocol 2)))
(allow file-read* file-write-data file-ioctl
  (literal "/dev/null")
  (literal "/dev/zero")
  (literal "/dev/dtracehelper"))
(allow file-read-data file-write-data (subpath "/dev/fd"))
`
