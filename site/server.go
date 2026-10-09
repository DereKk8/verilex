package main

import (
	"embed"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

//go:embed web
var web embed.FS

// NewServer serves the web app, the docs bundle read fresh on every load, and Ask.
// addr is the address the server listens on: on a loopback address it answers only requests
// addressed to a loopback host, so a page on another site cannot reach it through DNS rebinding.
func NewServer(load func() (*Bundle, error), providers *Providers, addr string) http.Handler {
	files, _ := fs.Sub(web, "web")
	static := http.FileServerFS(files)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /docs-data.js", func(w http.ResponseWriter, r *http.Request) {
		b, err := load()
		if err != nil {
			log.Print(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		script, err := b.Script()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(script)
	})
	mux.HandleFunc("GET /api/status", statusHandler(providers))
	mux.Handle("POST /api/ask", ownPagesOnly(askHandler(providers, load)))
	mux.Handle("POST /api/login", ownPagesOnly(loginHandler(providers)))
	mux.Handle("POST /api/login/input", ownPagesOnly(loginInputHandler(providers)))
	mux.Handle("GET /", static)

	loopback := isLoopback(addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if loopback && !isLoopback(r.Host) {
			http.Error(w, "this server answers only on localhost", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		mux.ServeHTTP(w, r)
	})
}

// ownPagesOnly admits JSON requests that the browser marks as coming from this server's own pages.
// A cross-site form or a no-cors fetch cannot send application/json without a preflight, which
// this server never answers, so another site cannot spend a plan or start a sign-in.
func ownPagesOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			http.Error(w, "send the request as application/json", http.StatusUnsupportedMediaType)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "Ask answers only pages served from this address", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin accepts a request that the browser marks as coming from this server's own pages.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			return false
		}
	}
	return true
}

// isLoopback reports whether a host or host:port names this machine's loopback interface.
func isLoopback(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
