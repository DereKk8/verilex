package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeCLIs puts stand-ins for claude and codex on PATH that answer like the real CLIs and record
// their arguments, stdin and whether an API key reached them, under log.
func fakeCLIs(t *testing.T) (log string) {
	t.Helper()
	bin, log := t.TempDir(), t.TempDir()
	write(t, bin, "claude", `#!/bin/sh
case "$1 $2" in
"auth status")
  [ -n "$FAKE_LOGGED_OUT" ] && { echo '{"loggedIn":false,"authMethod":"none"}'; exit 1; }
  echo '{"loggedIn":true,"authMethod":"claude.ai"}'; exit 0 ;;
"auth login")
  echo "Opening browser to sign in…"
  echo "If the browser didn't open, visit: https://claude.example/oauth/authorize?code=true"
  printf 'Paste code here if prompted > '
  read code; echo "signed in with $code"; exit 0 ;;
esac
for a in "$@"; do printf '%s\n' "$a"; done > "$FAKE_LOG/claude.args"
echo "${ANTHROPIC_API_KEY:-unset}" > "$FAKE_LOG/claude.key"
cat > "$FAKE_LOG/claude.stdin"
if [ -n "$FAKE_FAIL" ]; then
  echo '{"type":"result","subtype":"success","is_error":true,"result":"Invalid API key · Please run /login"}'; exit 1
fi
echo '{"type":"system","subtype":"init","model":"claude-opus-5-5"}'
echo '{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}'
echo '{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text"}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Skipped when "}}}'
echo '{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"every stamp matches."}}}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"Skipped when every stamp matches."}'
`)
	write(t, bin, "codex", `#!/bin/sh
case "$1 $2" in
"login status") echo "Logged in using ChatGPT"; exit 0 ;;
"login --device-auth") echo "Open https://auth.example/codex/device and enter this code: ABCD-12345"; exit 0 ;;
esac
for a in "$@"; do printf '%s\n' "$a"; done > "$FAKE_LOG/codex.args"
cat > "$FAKE_LOG/codex.stdin"
echo '{"type":"thread.started","thread_id":"t1"}'
echo '{"type":"item.started","item":{"id":"i0","type":"reasoning","text":""}}'
echo '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"Every stamp must match."}}'
echo '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}'
`)
	for _, f := range []string{"claude", "codex"} {
		if err := os.Chmod(filepath.Join(bin, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-reach-the-plan")
	return log
}

func planServer(t *testing.T, local bool) http.Handler {
	t.Helper()
	b := repoDocs(t)
	ps := NewProviders(local, claudeCode("", "low"), codex("", "low"), grok(nil), &apiProvider{Model: defaultModel, Effort: defaultEffort})
	return NewServer(func() (*Bundle, error) { return b, nil }, ps, "127.0.0.1:4173")
}

func events(body string) []string {
	var out []string
	for _, block := range strings.Split(strings.TrimSpace(body), "\n\n") {
		ev, data, _ := strings.Cut(block, "\n")
		out = append(out, strings.TrimPrefix(ev, "event: ")+" "+strings.TrimPrefix(data, "data: "))
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStatusListsEveryWayToAnswer(t *testing.T) {
	fakeCLIs(t)
	h := planServer(t, true)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:4173/api/status", nil))
	var got struct {
		Local     bool            `json:"local"`
		Providers []ProviderState `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err, w.Body.String())
	}
	var lines []string
	for _, p := range got.Providers {
		signed := "unknown"
		if p.SignedIn != nil {
			signed = map[bool]string{true: "signed in", false: "signed out"}[*p.SignedIn]
		}
		lines = append(lines, strings.Join([]string{p.ID, p.Kind, map[bool]string{true: "installed", false: "missing"}[p.Installed], signed, p.Detail}, " | "))
	}
	want := []string{
		"claude | subscription | installed | signed in | signed in with claude.ai",
		"codex | subscription | installed | signed in | logged in using ChatGPT",
		"grok | subscription | missing | signed out | grok is not installed",
		"api | api | installed | signed in | claude-opus-5-5 at low effort",
	}
	if !got.Local || strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("local %v, providers:\n%s\nwant:\n%s", got.Local, strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestClaudeCodeAnswersOnThePlanWithNoTools(t *testing.T) {
	log := fakeCLIs(t)
	w := ask(planServer(t, true), `{"provider":"claude","messages":[{"role":"user","content":"When is a chain skipped?"}],"page":"skipping"}`, nil)
	want := []string{
		`meta {"provider":"claude","via":"Claude Code"}`,
		`meta {"model":"claude-opus-5-5"}`,
		`status {"state":"thinking"}`,
		`status {"state":"writing"}`,
		`delta {"text":"Skipped when "}`,
		`delta {"text":"every stamp matches."}`,
		`done {}`,
	}
	if got := events(w.Body.String()); !slices.Equal(got, want) {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	args := strings.Split(strings.TrimSpace(readFile(t, filepath.Join(log, "claude.args"))), "\n")
	for _, pair := range [][2]string{{"--tools", ""}, {"--disallowedTools", "mcp__*"}, {"--effort", "low"}, {"--output-format", "stream-json"}} {
		i := slices.Index(args, pair[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
			t.Errorf("claude args miss %s %q: %q", pair[0], pair[1], args)
		}
	}
	if !slices.Contains(args, "--strict-mcp-config") || !slices.Contains(args, "--no-session-persistence") || !slices.Contains(args, "--system-prompt-file") {
		t.Errorf("claude args: %q", args)
	}
	if key := strings.TrimSpace(readFile(t, filepath.Join(log, "claude.key"))); key != "unset" {
		t.Errorf("the API key reached Claude Code, which would bill it instead of the plan: %q", key)
	}
	if stdin := readFile(t, filepath.Join(log, "claude.stdin")); stdin != "(I am reading the page `skipping`.)\n\nWhen is a chain skipped?" {
		t.Errorf("claude stdin %q", stdin)
	}
}

func TestClaudeCodeSignedOutSaysHowToSignIn(t *testing.T) {
	fakeCLIs(t)
	t.Setenv("FAKE_FAIL", "1")
	w := ask(planServer(t, true), `{"provider":"claude","messages":[{"role":"user","content":"hi"}]}`, nil)
	got := events(w.Body.String())
	want := "error {\"message\":\"Claude Code is not signed in. Use Sign in in the Ask menu, or run `claude auth login` in a terminal\"}"
	if got[len(got)-1] != want {
		t.Errorf("last event %s, want %s", got[len(got)-1], want)
	}
}

func TestCodexAnswersOnThePlanReadOnly(t *testing.T) {
	log := fakeCLIs(t)
	body := `{"provider":"codex","messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"When is a chain skipped?"}]}`
	w := ask(planServer(t, true), body, nil)
	want := []string{
		`meta {"provider":"codex","via":"Codex CLI"}`,
		`status {"state":"thinking"}`,
		`status {"state":"writing"}`,
		`delta {"text":"Every stamp must match."}`,
		`done {}`,
	}
	if got := events(w.Body.String()); !slices.Equal(got, want) {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	args := readFile(t, filepath.Join(log, "codex.args"))
	for _, a := range []string{"exec\n--json\n", "--sandbox\nread-only\n", "--ephemeral\n", "--skip-git-repo-check\n", "model_reasoning_effort=low\n"} {
		if !strings.Contains(args, a) {
			t.Errorf("codex args miss %q:\n%s", a, args)
		}
	}
	stdin := readFile(t, filepath.Join(log, "codex.stdin"))
	if !strings.Contains(stdin, "## The shared ledger {#the-shared-ledger}") || !strings.HasSuffix(stdin, "Reader: a\n\nYou: b\n\nThe reader's new question:\n\nWhen is a chain skipped?") {
		t.Errorf("codex stdin misses the docs or the conversation: ...%q", stdin[max(0, len(stdin)-200):])
	}
}

func TestPlansAnswerOnlyOnLocalhost(t *testing.T) {
	fakeCLIs(t)
	h := planServer(t, false)
	w := ask(h, `{"provider":"claude","messages":[{"role":"user","content":"hi"}]}`, nil)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "answers only on a server that listens on localhost") {
		t.Errorf("ask: %d %s", w.Code, w.Body)
	}
	w = ask(h, `{"provider":"codex"}`, func(r *http.Request) { r.URL.Path = "/api/login" })
	if w.Code != 400 {
		t.Errorf("login: %d %s", w.Code, w.Body)
	}
}

// Sign in runs the vendor's own command, shows the URL it prints, and types the code the person
// pastes into it.
func TestSignInRunsTheVendorFlow(t *testing.T) {
	fakeCLIs(t)
	t.Setenv("FAKE_LOGGED_OUT", "1")
	srv := httptest.NewServer(planServer(t, true))
	defer srv.Close()
	post := func(path, body string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := post("/api/login", `{"provider":"claude"}`)
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	var got []string
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		event := strings.TrimPrefix(line, "event: ")
		sc.Scan()
		data := strings.TrimPrefix(sc.Text(), "data: ")
		if event == "url" || event == "exit" || event == "status" {
			got = append(got, event+" "+data)
		}
		if event == "output" && strings.Contains(data, "Paste code here") {
			if r := post("/api/login/input", `{"provider":"claude","text":"CODE-42"}`); r.StatusCode != 204 {
				t.Fatalf("input: %d", r.StatusCode)
			}
		}
		if event == "output" && strings.Contains(data, "signed in with CODE-42") {
			got = append(got, "typed CODE-42")
		}
	}
	want := []string{
		`url {"url":"https://claude.example/oauth/authorize?code=true"}`,
		"typed CODE-42",
		`exit {"ok":true}`,
		`status {"installed":true,"signedIn":false,"detail":"not signed in","canLogin":true}`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("sign-in:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
