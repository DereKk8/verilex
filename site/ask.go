package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/config"
)

// Limits on one question, so a page cannot turn the server into an open relay for long prompts.
const (
	maxTurns        = 24
	maxQuestion     = 4000
	maxConversation = 32000
)

type askRequest struct {
	Provider string `json:"provider"`
	Messages []Turn `json:"messages"`
	Page     string `json:"page"`
}

// askHandler streams one answer as server-sent events from the provider the page chose:
// meta, status, delta (text), then done or error.
func askHandler(providers *Providers, docs func() (*Bundle, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req askRequest
		r.Body = http.MaxBytesReader(w, r.Body, 2*maxConversation)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "the request body is not a JSON question", http.StatusBadRequest)
			return
		}
		if err := checkConversation(req.Messages); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		p, err := providers.Get(req.Provider)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b, err := docs()
		if err != nil {
			http.Error(w, "the docs could not be read: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, _ := w.(http.Flusher)
		emit := func(event string, data any) {
			body, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
			if flusher != nil {
				flusher.Flush()
			}
		}
		d := p.Describe()
		emit("meta", map[string]string{"provider": d.ID, "via": d.Via})
		err = p.Ask(r.Context(), Question{Turns: req.Messages, Page: req.Page, Docs: b}, emit)
		switch {
		case errors.Is(err, context.Canceled):
		case err != nil:
			emit("error", map[string]string{"message": err.Error()})
		default:
			emit("done", map[string]string{})
		}
	}
}

// checkConversation accepts turns that start and end with the reader, alternate, and fit the limits.
func checkConversation(turns []Turn) error {
	n := len(turns)
	if n == 0 || n > maxTurns {
		return fmt.Errorf("send between 1 and %d turns", maxTurns)
	}
	total := 0
	for i, t := range turns {
		want := "user"
		if i%2 == 1 {
			want = "assistant"
		}
		text := strings.TrimSpace(t.Content)
		switch {
		case t.Role != want:
			return fmt.Errorf("turn %d is %q; turns alternate user and assistant, starting with user", i+1, t.Role)
		case text == "":
			return fmt.Errorf("turn %d is empty", i+1)
		case t.Role == "user" && len(text) > maxQuestion:
			return fmt.Errorf("a question holds at most %d characters", maxQuestion)
		}
		total += len(text)
	}
	if turns[n-1].Role != "user" {
		return errors.New("the last turn must be the question")
	}
	if total > maxConversation {
		return fmt.Errorf("the conversation holds at most %d characters; start a new one", maxConversation)
	}
	return nil
}

// apiProvider answers with the Claude API, billed to the key the server was started with.
type apiProvider struct {
	Client anthropic.Client
	Model  string
	Effort string
}

func (a *apiProvider) Describe() Description {
	return Description{ID: "api", Name: "Claude API", Plan: "Billed to the server's API key", Via: "Claude API", Kind: "api",
		SignIn: "export ANTHROPIC_API_KEY=… and restart scripts/docs"}
}

func (a *apiProvider) Status(context.Context) Availability {
	if !CredentialsFound() {
		return Availability{Installed: true, SignedIn: no(), Detail: "no ANTHROPIC_API_KEY or `ant auth login` profile"}
	}
	return Availability{Installed: true, SignedIn: yes(), Detail: a.Model + " at " + a.Effort + " effort"}
}

// CredentialsFound reports whether the SDK's credential chain has anything to use: an API key, an
// auth token, a named profile, workload identity federation or a profile from `ant auth login`.
func CredentialsFound() bool {
	for _, env := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE", "ANTHROPIC_FEDERATION_RULE_ID"} {
		if os.Getenv(env) != "" {
			return true
		}
	}
	_, err := config.LoadConfig()
	return err == nil
}

func (a *apiProvider) Ask(ctx context.Context, q Question, emit Emitter) error {
	var messages []anthropic.BetaMessageParam
	for i, t := range q.Turns {
		text := t.Content
		if i == len(q.Turns)-1 && q.Page != "" {
			text = "(I am reading the page `" + q.Page + "`.)\n\n" + text
		}
		block := anthropic.NewBetaTextBlock(text)
		if t.Role == "user" {
			messages = append(messages, anthropic.NewBetaUserMessage(block))
		} else {
			messages = append(messages, anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{block}})
		}
	}
	stream := a.Client.Beta.Messages.NewStreaming(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.Model),
		MaxTokens: 16000,
		System: []anthropic.BetaTextBlockParam{
			{Text: instructions},
			// The docs are the same for every question, so they are cached and only the question is new.
			{Text: corpus(q.Docs), CacheControl: anthropic.NewBetaCacheControlEphemeralParam()},
		},
		Messages:     messages,
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(a.Effort)},
		Fallbacks:    anthropic.BetaFallbacksParamOfDefault(),
		Betas:        []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	defer stream.Close()

	var stop anthropic.BetaStopReason
	for stream.Next() {
		switch ev := stream.Current().AsAny().(type) {
		case anthropic.BetaRawMessageStartEvent:
			emit("meta", map[string]string{"model": string(ev.Message.Model)})
		case anthropic.BetaRawContentBlockStartEvent:
			switch ev.ContentBlock.Type {
			case "thinking", "redacted_thinking":
				emit("status", map[string]string{"state": "thinking"})
			case "text":
				emit("status", map[string]string{"state": "writing"})
			}
		case anthropic.BetaRawContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.BetaTextDelta); ok && d.Text != "" {
				emit("delta", map[string]string{"text": d.Text})
			}
		case anthropic.BetaRawMessageDeltaEvent:
			if ev.Delta.StopReason != "" {
				stop = ev.Delta.StopReason
			}
		}
	}
	if err := stream.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return notice(describe(err))
	}
	if stop == anthropic.BetaStopReasonRefusal {
		return notice("Claude declined to answer this question. Try rephrasing it, or search the docs")
	}
	return nil
}

// notice is an error the web app shows the reader as written, so it reads as a sentence.
type notice string

func (n notice) Error() string { return string(n) }

func describe(err error) string {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "The Claude API refused the credentials. Check ANTHROPIC_API_KEY and restart scripts/docs"
		case http.StatusTooManyRequests:
			return "The Claude API is rate limiting this key. Wait a moment and ask again"
		}
		if apiErr.StatusCode >= 500 {
			return "The Claude API is unavailable right now. Ask again in a moment"
		}
		return fmt.Sprintf("The Claude API answered %d", apiErr.StatusCode)
	}
	return "The answer stopped: " + err.Error()
}

const instructions = `You answer questions about verilex, a CLI that runs product verification as chains of words, for the people who operate it. Answer only from the verilex documentation that follows. It is the specification: when it does not say, say that it does not, and point to the closest page.

Write for a busy operator:
- Lead with the answer in one or two sentences, then only the detail that supports it.
- Use the documentation's own terms, commands, flags and output. Never invent a command, flag, field, path or output line.
- Put commands, chains, configuration and output in fenced code blocks.
- Cite the pages you used as markdown links whose target is doc:<page>#<anchor>, for example [Proof stamps and skipping](doc:skipping#the-shared-ledger). Each heading below carries its anchor in {#...}. Link a page without an anchor as doc:<page>.
- Keep it short. Use headings only when the answer covers several separate topics.`

// corpus is every page of the bundle, each heading tagged with its anchor so answers can cite it.
func corpus(b *Bundle) string {
	var s strings.Builder
	s.WriteString("# The verilex documentation\n\n")
	for _, p := range b.Pages {
		fmt.Fprintf(&s, "<page id=%q title=%q section=%q>\n", p.Slug, p.Title, p.Section)
		hs := p.Headings
		for _, l := range mdLines(p.Markdown) {
			line := l.Text
			if !l.Fenced && headingLine.MatchString(line) && len(hs) > 0 {
				line = fmt.Sprintf("%s {#%s}", line, hs[0].ID)
				hs = hs[1:]
			}
			s.WriteString(line)
			s.WriteByte('\n')
		}
		s.WriteString("</page>\n\n")
	}
	return s.String()
}
