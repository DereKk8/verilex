// Command site is verilex's documentation as a web app: it serves docs/ with search and answers
// from Claude, or builds it as static files.
//
//	scripts/docs                    serve on http://127.0.0.1:4173
//	scripts/docs build -o DIR       write a static site to DIR (search and quoted answers, no Claude)
//	scripts/docs check              validate the docs: index, titles, links and anchors
//
// It is a module of its own, outside the verilex workspace, so the core keeps no dependencies.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	defaultRepo   = "https://github.com/DereKk8/verilex"
	defaultModel  = "claude-opus-5-5"
	defaultEffort = "low"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("docs: ")
	cmd, args := "serve", os.Args[1:]
	if len(args) > 0 && (args[0] == "serve" || args[0] == "build" || args[0] == "check") {
		cmd, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", "", "repository root (default: the nearest directory above holding docs/README.md)")
	repo := fs.String("repo", defaultRepo, "repository URL for source links")
	branch := fs.String("branch", "main", "branch for source links")
	addr := fs.String("addr", "127.0.0.1:4173", "serve: address to listen on")
	model := fs.String("model", envOr("VERILEX_DOCS_MODEL", defaultModel), "serve: Claude API model for answers billed to ANTHROPIC_API_KEY")
	effort := fs.String("effort", envOr("VERILEX_DOCS_EFFORT", defaultEffort), "serve: effort for answers: low, medium, high, xhigh or max")
	claudeModel := fs.String("claude-model", "", "serve: model Claude Code answers with on your plan (default: Claude Code's own)")
	codexModel := fs.String("codex-model", "", "serve: model Codex answers with on your ChatGPT plan (default: Codex's own)")
	grokCmd := fs.String("grok", "grok -p {prompt}", "serve: Grok Build's headless command; {prompt} is replaced by the prompt")
	noAI := fs.Bool("no-ai", false, "serve: answer by quoting the docs only, never call a model")
	out := fs.String("o", "", "build: output directory (default: site/dist in the repository)")
	_ = fs.Parse(args)

	if *root == "" {
		found, err := findRoot()
		if err != nil {
			log.Fatal(err)
		}
		*root = found
	}
	load := func() (*Bundle, error) { return Load(*root, *repo, *branch) }

	switch cmd {
	case "check":
		b, err := load()
		if err != nil {
			log.Fatal(err)
		}
		if errs := Validate(b, *root); len(errs) > 0 {
			for _, err := range errs {
				log.Print(err)
			}
			os.Exit(1)
		}
		fmt.Printf("docs: %d pages in %d sections, every link and anchor resolves\n", len(b.Pages), len(b.Sections))
	case "build":
		b, err := load()
		if err != nil {
			log.Fatal(err)
		}
		if errs := Validate(b, *root); len(errs) > 0 {
			for _, err := range errs {
				log.Print(err)
			}
			os.Exit(1)
		}
		dir := *out
		if dir == "" {
			dir = filepath.Join(*root, "site", "dist")
		}
		if err := Build(b, dir); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("docs: built %d pages into %s; open %s\n", len(b.Pages), dir, filepath.Join(dir, "index.html"))
	default:
		if b, err := load(); err != nil {
			log.Fatal(err)
		} else if errs := Validate(b, *root); len(errs) > 0 {
			for _, err := range errs {
				log.Print("warning: ", err)
			}
		}
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		local := isLoopback(ln.Addr().String())
		providers := NewProviders(local)
		if !*noAI {
			providers = NewProviders(local,
				claudeCode(*claudeModel, *effort),
				codex(*codexModel, *effort),
				grok(strings.Fields(*grokCmd)),
				&apiProvider{Client: anthropic.NewClient(), Model: *model, Effort: *effort},
			)
		}
		srv := &http.Server{
			Handler:           NewServer(load, providers, ln.Addr().String()),
			ReadHeaderTimeout: 10 * time.Second,
		}
		fmt.Printf("docs: serving http://%s\n", ln.Addr())
		for _, st := range providers.States(context.Background(), true) {
			state := "not signed in"
			switch {
			case !st.Enabled:
				state = "off: " + st.Reason
			case !st.Installed:
				state = "not installed"
			case st.SignedIn == nil || *st.SignedIn:
				state = st.Detail
			}
			fmt.Printf("docs: Ask with %-11s %s\n", st.Name+":", state)
		}
		if !local {
			fmt.Printf("docs: warning: %s is reachable from other machines; plans are off, and the Claude API key answers anyone who reaches it\n", ln.Addr())
		}
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// findRoot walks up from the working directory to the repository that holds docs/README.md.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "docs", "README.md")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no docs/README.md in this directory or above it; pass -root")
		}
		dir = parent
	}
}

// Build writes the static site: the web app, the bundle, and nothing that needs a server.
func Build(b *Bundle, dir string) error {
	static := *b
	static.Static = true
	script, err := static.Script()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := web.ReadDir("web")
	if err != nil {
		return err
	}
	for _, e := range entries {
		src, err := web.Open("web/" + e.Name())
		if err != nil {
			return err
		}
		dst, err := os.Create(filepath.Join(dir, e.Name()))
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "docs-data.js"), script, 0o644)
}
