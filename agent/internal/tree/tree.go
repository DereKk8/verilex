// Package tree records what a project looked like, so the launcher can tell whether the brain
// changed the code verilex verified. A verdict on code the brain edited is not the spec's verdict.
package tree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// State maps each file git tracks or would track (ignored files are left out) to its stat.
// The stat includes the inode change time, which no unprivileged process can set back, so an
// edit that restores the old bytes and mtime still shows.
type State map[string]string

// Snapshot reads project's state. The project must be in a git work tree: git's ignore rules
// separate the product's files from build output a run may write.
func Snapshot(project string) (State, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = project
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.Join(strings.Fields(stderr.String()), " ")
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s must be in a git work tree, so the launcher can see what the brain changed: %s", project, detail)
	}
	state := State{}
	for _, path := range strings.Split(string(out), "\x00") {
		if path != "" {
			state[path] = stat(filepath.Join(project, path))
		}
	}
	return state, nil
}

// Changed lists, sorted, every path added, removed or touched between before and after.
func Changed(before, after State) []string {
	var paths []string
	for path, key := range before {
		if after[path] != key {
			paths = append(paths, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

func stat(path string) string {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "error " + err.Error()
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
	}
	return fmt.Sprintf("%v %d %d %d %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano(), changeTime(st), st.Ino, st.Dev)
}
