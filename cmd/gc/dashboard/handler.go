// Package dashboard serves the static GC dashboard SPA.
package dashboard

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Embed the compiled SPA bundle produced by `cmd/gc/dashboard/web/`.
// The bundle is a Vite build output: one index.html (with a
// `<meta name="supervisor-url">` placeholder), one dashboard.js,
// one dashboard.css. The Go static server ships these bytes
// verbatim — the SPA handles everything else by calling the
// supervisor's typed OpenAPI endpoints directly.
//
//go:embed web/dist
var spaBundle embed.FS

const maxClientLogBody = 64 << 10

// reservedNonSPAPrefixes are URL prefixes the dashboard server never serves as
// SPA index.html. API prefixes are handled explicitly by the supervisor proxy.
var reservedNonSPAPrefixes = []string{
	"/api/",
	"/debug/",
}

type clientLogEntry struct {
	City    string          `json:"city"`
	Details json.RawMessage `json:"details,omitempty"`
	Level   string          `json:"level"`
	Message string          `json:"message"`
	Scope   string          `json:"scope"`
	TS      string          `json:"ts"`
	URL     string          `json:"url"`
}

// NewStaticHandler returns a handler that serves the SPA bundle.
// `supervisorURL` is the server-side upstream. The browser talks to the
// dashboard origin only; /v0/* and /health are proxied here so clients do not
// need direct routing to the supervisor bind address.
func NewStaticHandler(supervisorURL string) (http.Handler, error) {
	sub, err := fs.Sub(spaBundle, "web/dist")
	if err != nil {
		return nil, fmt.Errorf("dashboard: embed sub fs: %w", err)
	}
	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, fmt.Errorf("dashboard: read embedded index.html: %w", err)
	}
	indexWithURL := injectSupervisorURL(indexBytes, "")
	var supervisorProxy http.Handler
	if strings.TrimSpace(supervisorURL) != "" {
		supervisorProxy, err = newSupervisorProxy(supervisorURL)
		if err != nil {
			return nil, err
		}
	}

	fileServer := http.FileServer(http.FS(sub))

	mux := http.NewServeMux()
	mux.HandleFunc("/__client-log", handleClientLog)
	if supervisorProxy != nil {
		mux.Handle("/v0/", supervisorProxy)
		mux.Handle("/health", supervisorProxy)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		for _, p := range reservedNonSPAPrefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				http.NotFound(w, r)
				return
			}
		}
		if path == "" || path == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			_, _ = w.Write(indexWithURL)
			return
		}
		if _, err := fs.Stat(sub, path); err == nil {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			fileServer.ServeHTTP(w, r)
			return
		}
		// Unknown path under an SPA: serve index and let client-side
		// code figure out what to render (e.g. a "not found" panel).
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexWithURL)
	})

	return mux, nil
}

func newSupervisorProxy(supervisorURL string) (http.Handler, error) {
	upstream, err := url.Parse(supervisorURL)
	if err != nil {
		return nil, fmt.Errorf("dashboard: parse supervisor URL %q: %w", supervisorURL, err)
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		return nil, fmt.Errorf("dashboard: supervisor URL must include scheme and host: %q", supervisorURL)
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	defaultDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalHost := r.Host
		defaultDirector(r)
		r.Host = upstream.Host
		r.Header.Set("X-Forwarded-Host", originalHost)
		r.Header.Set("X-Forwarded-Proto", "http")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("dashboard: supervisor proxy %s %s failed: %v", r.Method, r.URL.Path, err)
		http.Error(w, "supervisor proxy failed", http.StatusBadGateway)
	}
	return proxy, nil
}

func handleClientLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close() //nolint:errcheck

	var entry clientLogEntry
	if err := json.NewDecoder(io.LimitReader(r.Body, maxClientLogBody)).Decode(&entry); err != nil {
		log.Printf("dashboard: client log decode failed from %s: %v", r.RemoteAddr, err)
		http.Error(w, "invalid client log payload", http.StatusBadRequest)
		return
	}

	level := strings.TrimSpace(entry.Level)
	if level == "" {
		level = "info"
	}
	scope := strings.TrimSpace(entry.Scope)
	if scope == "" {
		scope = "client"
	}
	if strings.TrimSpace(entry.Message) == "" {
		http.Error(w, "missing client log message", http.StatusBadRequest)
		return
	}
	ts := strings.TrimSpace(entry.TS)
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339Nano)
	}

	log.Printf(
		"dashboard: client[%s] ts=%s scope=%s city=%q url=%q msg=%q details=%s ua=%q",
		level,
		ts,
		scope,
		entry.City,
		entry.URL,
		entry.Message,
		rawJSONDetails(entry.Details),
		r.UserAgent(),
	)

	w.WriteHeader(http.StatusNoContent)
}

// injectSupervisorURL rewrites the `<meta name="supervisor-url" content="…">`
// tag to embed the real supervisor URL. The SPA reads this at load
// time to construct its typed client. Kept as a byte-level edit so
// there is no HTML parse overhead and no risk of the template
// engine escaping the URL. Vite emits the meta tag in the
// self-closed form (`content="" />`), so we match both spellings
// defensively.
func injectSupervisorURL(index []byte, supervisorURL string) []byte {
	replacement := fmt.Sprintf(`<meta name="supervisor-url" content="%s">`, htmlEscape(supervisorURL))
	for _, placeholder := range []string{
		`<meta name="supervisor-url" content="" />`,
		`<meta name="supervisor-url" content=""/>`,
		`<meta name="supervisor-url" content="">`,
	} {
		if bytes.Contains(index, []byte(placeholder)) {
			return bytes.Replace(index, []byte(placeholder), []byte(replacement), 1)
		}
	}
	return index
}

// htmlEscape performs the minimal escape the supervisor URL
// actually needs — quotes and angle brackets — since the URL is
// embedded in a `content="..."` attribute. Using a bespoke escaper
// keeps this package free of template/html dependencies.
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		`&`, `&amp;`,
		`"`, `&quot;`,
		`<`, `&lt;`,
		`>`, `&gt;`,
	)
	return r.Replace(s)
}

func rawJSONDetails(details json.RawMessage) string {
	if len(details) == 0 {
		return "null"
	}
	trimmed := strings.TrimSpace(string(details))
	if trimmed == "" {
		return "null"
	}
	return trimmed
}

// logRequest is a thin middleware used by Serve.
func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("dashboard: %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
