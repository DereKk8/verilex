package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// A Provider answers Ask. Subscription providers run a CLI the person already signed in to with
// their own plan, on their own machine: the docs server never sees a token, it only starts the
// vendor's own command. The API provider calls the Claude API with a key.
type Provider interface {
	Describe() Description
	Status(ctx context.Context) Availability
	Ask(ctx context.Context, q Question, emit Emitter) error
}

// Description is what the web app shows about a provider before it asks anything.
type Description struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Plan    string `json:"plan"`
	Via     string `json:"via"`
	Kind    string `json:"kind"` // "subscription" or "api"
	Install string `json:"install,omitempty"`
	SignIn  string `json:"signIn,omitempty"`
}

// Availability is a provider's state right now. SignedIn is nil when only a question can tell.
type Availability struct {
	Installed bool   `json:"installed"`
	SignedIn  *bool  `json:"signedIn"`
	Detail    string `json:"detail,omitempty"`
	CanLogin  bool   `json:"canLogin"`
}

// Question is one Ask: the conversation so far, ending on the new question, and the page it came from.
type Question struct {
	Turns []Turn
	Page  string
	Docs  *Bundle
}

// Turn is one message of the conversation.
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Emitter sends one server-sent event to the page: meta, status, delta, done or error.
type Emitter func(event string, data any)

func yes() *bool { t := true; return &t }
func no() *bool  { f := false; return &f }

// cliProvider runs a vendor CLI in an empty scratch directory with every tool it can turn off
// turned off, so it can read the docs it is given and nothing else.
type cliProvider struct {
	desc   Description
	argv0  string
	status func(ctx context.Context, p *cliProvider) Availability
	ask    func(ctx context.Context, p *cliProvider, q Question, dir string, emit Emitter) error
	login  []string
	// scrub lists environment variables that would make the CLI bill an API key instead of the plan.
	scrub []string
	model string
	// effort is passed where the CLI takes one: answering from given docs needs little thinking.
	effort string
	// cmdline is the ask command for providers whose headless flags are not settled (Grok Build).
	cmdline []string
}

func (p *cliProvider) Describe() Description { return p.desc }

func (p *cliProvider) Status(ctx context.Context) Availability {
	if _, err := exec.LookPath(p.argv0); err != nil {
		return Availability{SignedIn: no(), Detail: p.argv0 + " is not installed"}
	}
	return p.status(ctx, p)
}

func (p *cliProvider) Ask(ctx context.Context, q Question, emit Emitter) error {
	if _, err := exec.LookPath(p.argv0); err != nil {
		return fmt.Errorf("%s is not installed on this machine: %s", p.desc.Via, p.desc.Install)
	}
	dir, err := os.MkdirTemp("", "verilex-docs-ask-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return p.ask(ctx, p, q, dir, emit)
}

// env is this process's environment without the variables that would switch the CLI off the plan.
func (p *cliProvider) env() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, s := range p.scrub {
			drop = drop || name == s
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// command builds one CLI run: its own process group, killed with everything it started when the
// page stops asking, and a hard limit so a stuck CLI cannot hold the server.
func (p *cliProvider) command(ctx context.Context, dir string, argv ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = p.env()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	return cmd, cancel
}

// stream runs cmd with input on stdin and hands each stdout line to onLine. It returns the tail
// of stderr with the exit error, so a failure can say what the CLI said.
func stream(cmd *exec.Cmd, input string, onLine func(string)) (string, error) {
	cmd.Stdin = strings.NewReader(input)
	var stderr tail
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
	_, _ = io.Copy(io.Discard, out)
	return stderr.String(), cmd.Wait()
}

// tail keeps the last 2 KB written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// failure turns a CLI that ended without an answer into the sentence the page shows.
func failure(p *cliProvider, said string, err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	low := strings.ToLower(said)
	for _, hint := range []string{"log in", "login", "logged in", "sign in", "signed in", "auth", "credential", "401", "403"} {
		if strings.Contains(low, hint) {
			return fmt.Errorf("%s is not signed in. Use Sign in in the Ask menu, or run `%s` in a terminal", p.desc.Via, p.desc.SignIn)
		}
	}
	if said == "" && err != nil {
		said = err.Error()
	}
	if len(said) > 300 {
		said = "…" + said[len(said)-300:]
	}
	return fmt.Errorf("%s stopped without an answer: %s", p.desc.Via, said)
}

// --- Claude Code: the person's Claude plan ---

func claudeCode(model, effort string) *cliProvider {
	return &cliProvider{
		desc: Description{
			ID: "claude", Name: "Claude", Plan: "Your Claude plan (Pro, Max, Team or Enterprise)", Via: "Claude Code", Kind: "subscription",
			Install: "npm install -g @anthropic-ai/claude-code", SignIn: "claude auth login",
		},
		argv0: "claude", login: []string{"claude", "auth", "login"},
		scrub: []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}, model: model, effort: effort,
		status: func(ctx context.Context, p *cliProvider) Availability {
			cmd, cancel := p.command(ctx, os.TempDir(), "claude", "auth", "status")
			defer cancel()
			out, err := cmd.Output()
			var s struct {
				LoggedIn   bool   `json:"loggedIn"`
				AuthMethod string `json:"authMethod"`
				Email      string `json:"email"`
			}
			if jerr := json.Unmarshal(out, &s); jerr != nil {
				if err != nil {
					return Availability{Installed: true, SignedIn: no(), CanLogin: true, Detail: "not signed in"}
				}
				return Availability{Installed: true, CanLogin: true, Detail: "could not read `claude auth status`"}
			}
			if !s.LoggedIn {
				return Availability{Installed: true, SignedIn: no(), CanLogin: true, Detail: "not signed in"}
			}
			detail := map[string]string{"claude.ai": "signed in with claude.ai", "oauth_token": "signed in with a Claude token",
				"api_key": "signed in with an API key", "third_party": "signed in through a cloud provider"}[s.AuthMethod]
			if s.Email != "" {
				detail = "signed in as " + s.Email
			}
			if detail == "" {
				detail = "signed in"
			}
			return Availability{Installed: true, SignedIn: yes(), CanLogin: true, Detail: detail}
		},
		ask: func(ctx context.Context, p *cliProvider, q Question, dir string, emit Emitter) error {
			system := filepath.Join(dir, "system.md")
			if err := os.WriteFile(system, []byte(instructions+"\n\n"+corpus(q.Docs)), 0o600); err != nil {
				return err
			}
			argv := []string{"claude", "-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
				"--no-session-persistence", "--tools", "", "--strict-mcp-config", "--disallowedTools", "mcp__*",
				"--system-prompt-file", system}
			if p.model != "" {
				argv = append(argv, "--model", p.model)
			}
			if p.effort != "" {
				argv = append(argv, "--effort", p.effort)
			}
			cmd, cancel := p.command(ctx, dir, argv...)
			defer cancel()
			wrote, result, failed := false, "", ""
			said, err := stream(cmd, transcript(q, false), func(line string) {
				var ev struct {
					Type    string `json:"type"`
					Subtype string `json:"subtype"`
					Model   string `json:"model"`
					IsError bool   `json:"is_error"`
					Result  string `json:"result"`
					Event   struct {
						Type         string `json:"type"`
						ContentBlock struct {
							Type string `json:"type"`
						} `json:"content_block"`
						Delta struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"delta"`
					} `json:"event"`
				}
				if json.Unmarshal([]byte(line), &ev) != nil {
					return
				}
				switch {
				case ev.Type == "system" && ev.Subtype == "init" && ev.Model != "":
					emit("meta", map[string]string{"model": ev.Model})
				case ev.Type == "stream_event" && ev.Event.Type == "content_block_start":
					switch ev.Event.ContentBlock.Type {
					case "thinking", "redacted_thinking":
						emit("status", map[string]string{"state": "thinking"})
					case "text":
						emit("status", map[string]string{"state": "writing"})
					}
				case ev.Type == "stream_event" && ev.Event.Delta.Type == "text_delta" && ev.Event.Delta.Text != "":
					wrote = true
					emit("delta", map[string]string{"text": ev.Event.Delta.Text})
				case ev.Type == "result" && ev.IsError:
					failed = ev.Result
					if failed == "" {
						failed = ev.Subtype
					}
				case ev.Type == "result":
					result = ev.Result
				}
			})
			switch {
			case failed != "":
				return failure(p, failed, err)
			case !wrote && result != "":
				// A Claude Code without partial messages still reports the whole answer at the end.
				emit("delta", map[string]string{"text": result})
			case !wrote:
				return failure(p, said, err)
			}
			return nil
		},
	}
}

// --- Codex: the person's ChatGPT plan ---

func codex(model, effort string) *cliProvider {
	return &cliProvider{
		desc: Description{
			ID: "codex", Name: "ChatGPT", Plan: "Your ChatGPT plan (Plus, Pro, Business or Enterprise)", Via: "Codex CLI", Kind: "subscription",
			Install: "npm install -g @openai/codex", SignIn: "codex login",
		},
		argv0: "codex", login: []string{"codex", "login", "--device-auth"},
		scrub: []string{"CODEX_API_KEY"}, model: model, effort: effort,
		status: func(ctx context.Context, p *cliProvider) Availability {
			cmd, cancel := p.command(ctx, os.TempDir(), "codex", "login", "status")
			defer cancel()
			out, err := cmd.CombinedOutput()
			text := strings.TrimSpace(string(out))
			if line, _, _ := strings.Cut(text, "\n"); line != "" {
				text = line
			}
			if err != nil {
				return Availability{Installed: true, SignedIn: no(), CanLogin: true, Detail: "not signed in"}
			}
			if text == "" {
				text = "signed in"
			}
			return Availability{Installed: true, SignedIn: yes(), CanLogin: true, Detail: strings.ToLower(text[:1]) + text[1:]}
		},
		ask: func(ctx context.Context, p *cliProvider, q Question, dir string, emit Emitter) error {
			argv := []string{"codex", "exec", "--json", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-C", dir}
			if p.model != "" {
				argv = append(argv, "-m", p.model)
			}
			if p.effort != "" {
				argv = append(argv, "-c", "model_reasoning_effort="+p.effort)
			}
			cmd, cancel := p.command(ctx, dir, append(argv, "-")...)
			defer cancel()
			prompt := instructions + "\n\n" + corpus(q.Docs) + "\n\n" + transcript(q, true)
			wrote, failed := false, ""
			said, err := stream(cmd, prompt, func(line string) {
				var ev struct {
					Type    string `json:"type"`
					Message string `json:"message"`
					Error   struct {
						Message string `json:"message"`
					} `json:"error"`
					Item struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				if json.Unmarshal([]byte(line), &ev) != nil {
					return
				}
				switch {
				case ev.Type == "item.started" && ev.Item.Type == "reasoning":
					emit("status", map[string]string{"state": "thinking"})
				case ev.Type == "item.completed" && ev.Item.Type == "agent_message" && ev.Item.Text != "":
					text := ev.Item.Text
					if wrote {
						text = "\n\n" + text
					}
					wrote = true
					emit("status", map[string]string{"state": "writing"})
					emit("delta", map[string]string{"text": text})
				case ev.Type == "turn.failed":
					failed = ev.Error.Message
				case ev.Type == "error":
					failed = ev.Message
				}
			})
			switch {
			case failed != "":
				return failure(p, failed, err)
			case !wrote:
				return failure(p, said, err)
			}
			return nil
		},
	}
}

// --- Grok Build: the person's SuperGrok or X Premium+ plan ---

// The docs go into a file the agent reads, not into the prompt, because -p takes the prompt as
// one argument and an argument is limited in size. Override the command with -grok when your
// Grok Build differs: {prompt} is replaced by the prompt.
func grok(command []string) *cliProvider {
	if len(command) == 0 {
		command = []string{"grok", "-p", "{prompt}"}
	}
	return &cliProvider{
		desc: Description{
			ID: "grok", Name: "Grok", Plan: "Your SuperGrok or X Premium+ plan", Via: "Grok Build", Kind: "subscription",
			Install: "see x.ai for the Grok Build installer", SignIn: "grok",
		},
		argv0: command[0], cmdline: command, scrub: []string{"GROK_CODE_XAI_API_KEY", "XAI_API_KEY"},
		status: func(ctx context.Context, p *cliProvider) Availability {
			// Grok Build has no sign-in status command; the first question tells.
			return Availability{Installed: true, Detail: "sign-in is checked when you ask"}
		},
		ask: func(ctx context.Context, p *cliProvider, q Question, dir string, emit Emitter) error {
			if err := os.WriteFile(filepath.Join(dir, "verilex-docs.md"), []byte(corpus(q.Docs)), 0o600); err != nil {
				return err
			}
			prompt := instructions + "\n\nThe verilex documentation is in the file verilex-docs.md in the current directory. Read it before you answer, and change nothing.\n\n" + transcript(q, true)
			argv := make([]string, len(p.cmdline))
			for i, a := range p.cmdline {
				argv[i] = strings.ReplaceAll(a, "{prompt}", prompt)
			}
			cmd, cancel := p.command(ctx, dir, argv...)
			defer cancel()
			emit("status", map[string]string{"state": "thinking"})
			wrote := false
			said, err := stream(cmd, "", func(line string) {
				if !wrote {
					emit("status", map[string]string{"state": "writing"})
				} else {
					line = "\n" + line
				}
				wrote = true
				emit("delta", map[string]string{"text": line})
			})
			if err != nil || !wrote {
				return failure(p, said, err)
			}
			return nil
		},
	}
}

// transcript is the conversation as one prompt, for CLIs that take a single message.
func transcript(q Question, withHeader bool) string {
	var s strings.Builder
	if withHeader {
		s.WriteString("# The conversation\n\n")
	}
	n := len(q.Turns)
	if n > 1 {
		s.WriteString("Earlier in this conversation:\n\n")
		for _, t := range q.Turns[:n-1] {
			who := "Reader"
			if t.Role == "assistant" {
				who = "You"
			}
			fmt.Fprintf(&s, "%s: %s\n\n", who, t.Content)
		}
		s.WriteString("The reader's new question:\n\n")
	}
	if q.Page != "" {
		fmt.Fprintf(&s, "(I am reading the page `%s`.)\n\n", q.Page)
	}
	s.WriteString(q.Turns[n-1].Content)
	return s.String()
}
