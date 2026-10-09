package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func repoDocs(t *testing.T) *Bundle {
	t.Helper()
	b, err := Load("..", defaultRepo, "main")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The repository's own docs are the first thing the site checks: every page is in the index, every
// index title is its page's title, and every relative link and anchor resolves.
func TestRepositoryDocsValidate(t *testing.T) {
	b := repoDocs(t)
	for _, err := range Validate(b, "..") {
		t.Error(err)
	}
	if len(b.Sections) == 0 || len(b.Pages) < 10 || b.Intro == "" {
		t.Fatalf("bundle has %d sections, %d pages, intro %q", len(b.Sections), len(b.Pages), b.Intro)
	}
	if b.Pages[0].Slug != "getting-started" {
		t.Errorf("the first page is %s, want getting-started", b.Pages[0].Slug)
	}
}

func write(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNamesEveryBrokenLink(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "# x\n\nSee [a](docs/a.md#gone).\n")
	write(t, root, "docs/README.md", "# Docs\n\nIntro.\n\n## Start\n\n- [Alpha](a.md): the first page.\n")
	write(t, root, "docs/a.md", "# Alpha\n\n## Real `heading`\n\n[ok](#real-heading) [bad](#nope) [missing](b.md) "+
		"`[in code](c.md)` [out](../../x.md)\n\n```\n[fenced](d.md)\n```\n")
	write(t, root, "docs/orphan.md", "# Orphan\n")
	b, err := Load(root, defaultRepo, "main")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, err := range Validate(b, root) {
		got = append(got, err.Error())
	}
	want := []string{
		"docs/orphan.md is not listed in docs/README.md, so the site never shows it",
		"docs/a.md: link #nope: docs/a.md has no heading #nope",
		"docs/a.md: link b.md: docs/b.md does not exist",
		"docs/a.md: link ../../x.md leaves the repository",
		"README.md: link docs/a.md#gone: docs/a.md has no heading #gone",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("errors:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestIndexItemsMustBePageLinks(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/README.md", "# Docs\n\n## Start\n\n- Alpha, the first page\n")
	if _, err := Load(root, defaultRepo, "main"); err == nil || !strings.Contains(err.Error(), "is not a `- [Title](page.md): summary` item") {
		t.Fatalf("err = %v", err)
	}
}

func TestHeadingsMatchGitHubAnchors(t *testing.T) {
	hs := Headings("# Claims\n\n## Words pin a version\n\n```\n## not a heading\n```\n\n## The `.verilex` directory\n\n" +
		"## Words pin a version\n\n### [Linked](x.md) **bold**\n")
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.ID)
	}
	want := "claims words-pin-a-version the-verilex-directory words-pin-a-version-1 linked-bold"
	if strings.Join(ids, " ") != want {
		t.Errorf("ids %q, want %q", strings.Join(ids, " "), want)
	}
}

func TestBuildWritesAStaticSite(t *testing.T) {
	b := repoDocs(t)
	dir := filepath.Join(t.TempDir(), "out")
	if err := Build(b, dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "app.css", "app.js", "docs-data.js"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Error(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs-data.js"))
	if err != nil {
		t.Fatal(err)
	}
	var got Bundle
	body := strings.TrimSuffix(strings.TrimPrefix(string(data), "window.VERILEX_DOCS = "), ";\n")
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Static || len(got.Pages) != len(b.Pages) {
		t.Errorf("static %v, %d pages; want static and %d pages", got.Static, len(got.Pages), len(b.Pages))
	}
}

// fakeClaude answers like the Messages API's stream, and records the request it got.
func fakeClaude(t *testing.T, stop string, seen *map[string]any, header *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, seen)
		*header = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		events := []string{
			`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"A chain is skipped "}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"when every stamp matches."}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"` + stop + `","stop_sequence":null},"usage":{"output_tokens":12}}`,
			`{"type":"message_stop"}`,
		}
		for _, e := range events {
			var typ struct{ Type string }
			_ = json.Unmarshal([]byte(e), &typ)
			_, _ = io.WriteString(w, "event: "+typ.Type+"\ndata: "+e+"\n\n")
		}
	}))
}

func askServer(t *testing.T, upstream string) http.Handler {
	t.Helper()
	b := repoDocs(t)
	api := &apiProvider{
		Client: anthropic.NewClient(option.WithBaseURL(upstream), option.WithAPIKey("test-key"), option.WithMaxRetries(0)),
		Model:  defaultModel, Effort: defaultEffort,
	}
	return NewServer(func() (*Bundle, error) { return b, nil }, NewProviders(true, api), "127.0.0.1:4173")
}

func ask(h http.Handler, body string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4173/api/ask", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://127.0.0.1:4173")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const question = `{"provider":"api","messages":[{"role":"user","content":"When is a chain skipped?"}],"page":"skipping"}`

func TestAskStreamsAnAnswerGroundedInTheDocs(t *testing.T) {
	var seen map[string]any
	var header http.Header
	up := fakeClaude(t, "end_turn", &seen, &header)
	defer up.Close()
	w := ask(askServer(t, up.URL), question, nil)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	want := "event: meta\ndata: {\"provider\":\"api\",\"via\":\"Claude API\"}\n\n" +
		"event: meta\ndata: {\"model\":\"claude-opus-5-5\"}\n\n" +
		"event: status\ndata: {\"state\":\"thinking\"}\n\n" +
		"event: status\ndata: {\"state\":\"writing\"}\n\n" +
		"event: delta\ndata: {\"text\":\"A chain is skipped \"}\n\n" +
		"event: delta\ndata: {\"text\":\"when every stamp matches.\"}\n\n" +
		"event: done\ndata: {}\n\n"
	if w.Body.String() != want {
		t.Errorf("stream:\n%s\nwant:\n%s", w.Body, want)
	}

	if seen["model"] != defaultModel || seen["fallbacks"] != "default" {
		t.Errorf("model %v, fallbacks %v", seen["model"], seen["fallbacks"])
	}
	if !strings.Contains(header.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta %q", header.Get("Anthropic-Beta"))
	}
	if effort := seen["output_config"].(map[string]any)["effort"]; effort != defaultEffort {
		t.Errorf("effort %v", effort)
	}
	system := seen["system"].([]any)
	docs := system[1].(map[string]any)
	if docs["cache_control"] == nil || !strings.Contains(docs["text"].(string), "## The shared ledger {#the-shared-ledger}") {
		t.Errorf("the docs block is not cached or misses tagged headings: cache_control %v", docs["cache_control"])
	}
	msgs := seen["messages"].([]any)
	text := msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if text != "(I am reading the page `skipping`.)\n\nWhen is a chain skipped?" {
		t.Errorf("question %q", text)
	}
}

func TestAskReportsARefusal(t *testing.T) {
	var seen map[string]any
	var header http.Header
	up := fakeClaude(t, "refusal", &seen, &header)
	defer up.Close()
	w := ask(askServer(t, up.URL), question, nil)
	if !strings.HasSuffix(w.Body.String(), "event: error\ndata: {\"message\":\"Claude declined to answer this question. Try rephrasing it, or search the docs\"}\n\n") {
		t.Errorf("stream ends:\n%s", w.Body)
	}
}

func TestAskAnswersOnlyItsOwnPages(t *testing.T) {
	var seen map[string]any
	var header http.Header
	up := fakeClaude(t, "end_turn", &seen, &header)
	defer up.Close()
	h := askServer(t, up.URL)
	cases := []struct {
		name string
		body string
		edit func(*http.Request)
		code int
	}{
		{"another site", question, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"another origin", question, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"a form post", question, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"a rebound host", question, func(r *http.Request) { r.Host = "evil.example:4173" }, 421},
		{"no question", `{"provider":"api","messages":[]}`, nil, 400},
		{"assistant last", `{"provider":"api","messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"}]}`, nil, 400},
		{"too long", `{"provider":"api","messages":[{"role":"user","content":"` + strings.Repeat("x", maxQuestion+1) + `"}]}`, nil, 400},
		{"unknown provider", `{"provider":"nope","messages":[{"role":"user","content":"a"}]}`, nil, 400},
	}
	for _, c := range cases {
		if w := ask(h, c.body, c.edit); w.Code != c.code {
			t.Errorf("%s: status %d, want %d: %s", c.name, w.Code, c.code, w.Body)
		}
	}
	if seen != nil {
		t.Errorf("a refused request reached Claude: %v", seen["messages"])
	}
}

func TestDocsDataAndPages(t *testing.T) {
	b := repoDocs(t)
	h := NewServer(func() (*Bundle, error) { return b, nil }, NewProviders(true), "127.0.0.1:4173")
	for path, want := range map[string]string{
		"/api/status":   `{"local":true,"providers":[]}`,
		"/docs-data.js": "window.VERILEX_DOCS = {",
		"/":             "<!doctype html>",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost:4173"+path, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Body.String(), want) {
			t.Errorf("GET %s: %d %.60q, want prefix %q", path, w.Code, w.Body.String(), want)
		}
	}
	if w := ask(h, question, nil); w.Code != 400 {
		t.Errorf("ask with no providers: %d", w.Code)
	}
}
