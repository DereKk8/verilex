//go:build ignore

// Fails if the verilex core imports the agent module or names a specific harness.
// It reads cmd/ and internal/ as Go: imports, string literals and comments. Identifiers are not
// harness names (a loop index called pi drives nothing). Skill-directory literals are not harness
// drivers either: the core has to know where project skills live (.cursor/skills, .claude/skills).
// usage: go run scripts/boundary.go [ROOT]
package main

import (
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const agent = "github.com/DereKk8/verilex/agent"

var (
	harness   = regexp.MustCompile(`(^|[^A-Za-z0-9_])(claude-code|cursor-agent|opencode|antigravity|rovodev|copilot|grok|acpx|aider|gemini-cli|codex|claude|cursor|pi)([^A-Za-z0-9_]|$)`)
	skillDirs = strings.NewReplacer(".cursor/skills", "", ".claude/skills", "", ".agents/skills", "")
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	var imports, names []string
	scanned := 0
	for _, dir := range []string{"cmd", "internal"} {
		top := filepath.Join(root, dir)
		if _, err := os.Stat(top); err != nil {
			continue
		}
		scanned++
		err := filepath.WalkDir(top, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, src, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range file.Imports {
				if p, _ := strconv.Unquote(spec.Path.Value); p == agent || strings.HasPrefix(p, agent+"/") {
					imports = append(imports, fmt.Sprintf("%s:%d: %s", path, fset.Position(spec.Pos()).Line, p))
				}
			}
			var s scanner.Scanner
			s.Init(fset.AddFile(path, -1, len(src)), src, nil, scanner.ScanComments)
			for {
				pos, tok, lit := s.Scan()
				if tok == token.EOF {
					break
				}
				if tok != token.STRING && tok != token.COMMENT {
					continue
				}
				if m := harness.FindStringSubmatch(skillDirs.Replace(lit)); m != nil {
					names = append(names, fmt.Sprintf("%s:%d: %s", path, fset.Position(pos).Line, m[2]))
				}
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "boundary:", err)
			os.Exit(2)
		}
	}
	if scanned == 0 {
		fmt.Fprintf(os.Stderr, "boundary: no cmd/ or internal/ under %s\n", root)
		os.Exit(2)
	}
	if len(imports) > 0 {
		fmt.Println("core imports the agent module:")
		fmt.Println(strings.Join(imports, "\n"))
	}
	if len(names) > 0 {
		fmt.Println("core names a specific harness:")
		fmt.Println(strings.Join(names, "\n"))
	}
	if len(imports)+len(names) > 0 {
		os.Exit(1)
	}
	fmt.Println("boundary: ok")
}
