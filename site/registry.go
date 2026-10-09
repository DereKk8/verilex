package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"
)

// Providers is every way Ask can answer on this server, in the order the web app offers them.
type Providers struct {
	list []Provider
	// local is true when the server listens only on loopback. Subscriptions are personal, so a
	// server reachable from other machines never answers with one, and never starts a sign-in.
	local bool

	mu     sync.Mutex
	cached map[string]cachedStatus
	logins map[string]*loginSession
}

type cachedStatus struct {
	at time.Time
	a  Availability
}

// ProviderState is one provider as the web app lists it.
type ProviderState struct {
	Description
	Availability
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
}

// NewProviders lists the providers in the order the web app offers them.
func NewProviders(local bool, list ...Provider) *Providers {
	return &Providers{list: list, local: local, cached: map[string]cachedStatus{}, logins: map[string]*loginSession{}}
}

// statusTTL keeps one `claude auth status` from running on every page load.
const statusTTL = 20 * time.Second

// States checks every provider at once. A CLI that takes too long to answer reads as unknown.
func (ps *Providers) States(ctx context.Context, refresh bool) []ProviderState {
	out := make([]ProviderState, len(ps.list))
	var wg sync.WaitGroup
	for i, p := range ps.list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := p.Describe()
			st := ProviderState{Description: d, Availability: ps.status(ctx, p, refresh), Enabled: true}
			if d.Kind == "subscription" && !ps.local {
				st.Enabled, st.CanLogin = false, false
				st.Reason = "Plans are personal: this server listens beyond localhost, so it never answers with one. Run scripts/docs on your own machine."
			}
			out[i] = st
		}()
	}
	wg.Wait()
	return out
}

func (ps *Providers) status(ctx context.Context, p Provider, refresh bool) Availability {
	id := p.Describe().ID
	ps.mu.Lock()
	c, ok := ps.cached[id]
	ps.mu.Unlock()
	if ok && !refresh && time.Since(c.at) < statusTTL {
		return c.a
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	a := p.Status(ctx)
	ps.mu.Lock()
	ps.cached[id] = cachedStatus{time.Now(), a}
	ps.mu.Unlock()
	return a
}

func (ps *Providers) forget(id string) {
	ps.mu.Lock()
	delete(ps.cached, id)
	ps.mu.Unlock()
}

// Get returns the provider the page chose, if this server may answer with it.
func (ps *Providers) Get(id string) (Provider, error) {
	for _, p := range ps.list {
		d := p.Describe()
		if d.ID != id {
			continue
		}
		if d.Kind == "subscription" && !ps.local {
			return nil, fmt.Errorf("%s answers only on a server that listens on localhost", d.Via)
		}
		return p, nil
	}
	return nil, fmt.Errorf("no provider %q on this server", id)
}

func statusHandler(ps *Providers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"local":     ps.local,
			"providers": ps.States(r.Context(), r.URL.Query().Get("refresh") != ""),
		})
	}
}

// loginSession is one sign-in command the page started, so the page can type into it.
type loginSession struct {
	mu    sync.Mutex
	stdin io.WriteCloser
}

var (
	urlPattern  = regexp.MustCompile(`https://[^\s"'<>]+`)
	codePattern = regexp.MustCompile(`\b[A-Z0-9]{4,5}-[A-Z0-9]{4,5}\b`)
)

// loginHandler runs the vendor's own sign-in command and streams what it prints: output, each
// sign-in URL and device code it names, then exit and the provider's new status. The command
// opens the vendor's sign-in page itself where it can; the page shows the URL in case it cannot.
func loginHandler(ps *Providers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
			http.Error(w, "send {\"provider\": ...}", http.StatusBadRequest)
			return
		}
		p, err := ps.Get(req.Provider)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cli, ok := p.(*cliProvider)
		if !ok || len(cli.login) == 0 {
			http.Error(w, fmt.Sprintf("%s signs in from a terminal: %s", p.Describe().Via, p.Describe().SignIn), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		flusher, _ := w.(http.Flusher)
		var wmu sync.Mutex
		emit := func(event string, data any) {
			wmu.Lock()
			defer wmu.Unlock()
			body, _ := json.Marshal(data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
			if flusher != nil {
				flusher.Flush()
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		cmd, stop := cli.command(ctx, os.TempDir(), cli.login...)
		defer stop()
		stdin, err := cmd.StdinPipe()
		if err != nil {
			emit("error", map[string]string{"message": err.Error()})
			return
		}
		pr, pw, err := os.Pipe()
		if err != nil {
			emit("error", map[string]string{"message": err.Error()})
			return
		}
		cmd.Stdout, cmd.Stderr = pw, pw
		if err := cmd.Start(); err != nil {
			pw.Close()
			pr.Close()
			emit("error", map[string]string{"message": err.Error()})
			return
		}
		pw.Close()
		session := &loginSession{stdin: stdin}
		ps.mu.Lock()
		ps.logins[req.Provider] = session
		ps.mu.Unlock()
		defer func() {
			ps.mu.Lock()
			if ps.logins[req.Provider] == session {
				delete(ps.logins, req.Provider)
			}
			ps.mu.Unlock()
		}()

		seen := map[string]bool{}
		var all []byte
		buf := make([]byte, 4096)
		for {
			n, rerr := pr.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				all = append(all, chunk...)
				emit("output", map[string]string{"text": string(chunk)})
				for _, u := range urlPattern.FindAllString(string(all), -1) {
					if !seen[u] {
						seen[u] = true
						emit("url", map[string]string{"url": u})
					}
				}
				for _, c := range codePattern.FindAllString(string(all), -1) {
					if !seen[c] {
						seen[c] = true
						emit("code", map[string]string{"code": c})
					}
				}
			}
			if rerr != nil {
				break
			}
		}
		pr.Close()
		werr := cmd.Wait()
		emit("exit", map[string]bool{"ok": werr == nil})
		ps.forget(req.Provider)
		emit("status", ps.status(r.Context(), p, true))
	}
}

// loginInputHandler types one line into a running sign-in, such as the code Claude Code asks for.
func loginInputHandler(ps *Providers) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Provider string `json:"provider"`
			Text     string `json:"text"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req); err != nil || req.Text == "" {
			http.Error(w, "send {\"provider\": ..., \"text\": ...}", http.StatusBadRequest)
			return
		}
		ps.mu.Lock()
		session := ps.logins[req.Provider]
		ps.mu.Unlock()
		if session == nil {
			http.Error(w, "no sign-in is running for "+req.Provider, http.StatusConflict)
			return
		}
		session.mu.Lock()
		_, err := io.WriteString(session.stdin, req.Text+"\n")
		session.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
