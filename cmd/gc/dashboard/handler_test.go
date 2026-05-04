package dashboard

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInjectSupervisorURL verifies the meta-tag placeholder gets
// replaced with the real URL on page load. This is the only dynamic
// bit the Go static server owns.
func TestInjectSupervisorURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		orig string
		want string
	}{
		{
			name: "localhost non-selfclose",
			url:  "http://127.0.0.1:8372",
			orig: `<meta name="supervisor-url" content="">`,
			want: `<meta name="supervisor-url" content="http://127.0.0.1:8372">`,
		},
		{
			name: "vite self-closed form",
			url:  "http://127.0.0.1:8372",
			orig: `<meta name="supervisor-url" content="" />`,
			want: `<meta name="supervisor-url" content="http://127.0.0.1:8372">`,
		},
		{
			name: "html-escape in URL",
			url:  `http://example.com/?q="x"&y=<z>`,
			orig: `<meta name="supervisor-url" content="">`,
			want: `<meta name="supervisor-url" content="http://example.com/?q=&quot;x&quot;&amp;y=&lt;z&gt;">`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(injectSupervisorURL([]byte(tc.orig), tc.url))
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestStaticHandlerServesIndex confirms the handler serves same-origin index
// HTML and that dashboard.js is reachable.
func TestStaticHandlerServesIndex(t *testing.T) {
	h, err := NewStaticHandler("http://127.0.0.1:8372")
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}

	// Index.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<meta name="supervisor-url" content="">`) {
		t.Errorf("index should use same-origin supervisor URL; body:\n%s", body)
	}

	// Bundle.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /dashboard.js: %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("dashboard.js was empty")
	}

	// Unknown path falls back to index.html so the SPA's
	// client-side router (such as it is) can handle unknown
	// routes.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/unknown/deep/path", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("fallback GET: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<meta name="supervisor-url"`) {
		t.Errorf("fallback did not serve SPA index")
	}
}

func TestStaticHandlerProxiesSupervisorAPI(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if got := r.Header.Get("X-Forwarded-Host"); got == "" {
			t.Error("proxy did not set X-Forwarded-Host")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(upstream.Close)

	h, err := NewStaticHandler(upstream.URL)
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health: %d %s", rec.Code, rec.Body.String())
	}
	if seenPath != "/health" {
		t.Fatalf("proxied path = %q, want /health", seenPath)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("proxy body = %s", rec.Body.String())
	}
}

func TestStaticHandlerRejectsInvalidSupervisorURL(t *testing.T) {
	if _, err := NewStaticHandler("127.0.0.1:8372"); err == nil {
		t.Fatal("NewStaticHandler accepted supervisor URL without scheme")
	}
}

func TestStaticHandlerAcceptsClientLogs(t *testing.T) {
	h, err := NewStaticHandler("http://127.0.0.1:8372")
	if err != nil {
		t.Fatalf("NewStaticHandler: %v", err)
	}

	var logs bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/__client-log", strings.NewReader(`{
		"ts":"2026-04-17T16:00:00Z",
		"level":"error",
		"scope":"mail",
		"message":"Compose failed",
		"details":{"reason":"missing recipient"},
		"url":"http://localhost:8080/?city=mc-city",
		"city":"mc-city"
	}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /__client-log: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), `client[error]`) {
		t.Fatalf("client log output missing level: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `scope=mail`) {
		t.Fatalf("client log output missing scope: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"reason":"missing recipient"`) {
		t.Fatalf("client log output missing details: %s", logs.String())
	}
}
