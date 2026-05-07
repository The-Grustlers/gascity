package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/session"
	"github.com/gorilla/websocket"
)

type terminalAttachTestProvider struct {
	*runtime.Fake
	resizeCh chan [2]int
}

func (p *terminalAttachTestProvider) TerminalAttachCommand(string) (runtime.TerminalCommandSpec, error) {
	return runtime.TerminalCommandSpec{
		Path: "sh",
		Args: []string{"-c", "printf 'terminal ready\\n'; while IFS= read -r line; do printf 'echo:%s\\n' \"$line\"; done"},
	}, nil
}

func (p *terminalAttachTestProvider) TerminalResizeCommand(_ string, cols, rows int) (runtime.TerminalCommandSpec, error) {
	select {
	case p.resizeCh <- [2]int{cols, rows}:
	default:
	}
	return runtime.TerminalCommandSpec{}, nil
}

func TestHandleSessionTerminalWebSocketAttachesAndPipesInput(t *testing.T) {
	provider := &terminalAttachTestProvider{
		Fake:     runtime.NewFake(),
		resizeCh: make(chan [2]int, 1),
	}
	fs := newSessionFakeState(t)
	state := &stateWithSessionProvider{fakeState: fs, provider: provider}
	mgr := session.NewManager(fs.cityBeadStore, provider)
	info, err := mgr.Create(context.Background(), "default", "Director", "echo test", "/tmp", "test", nil, session.ProviderResume{}, runtime.Config{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	server := httptest.NewServer(newTestCityHandler(t, state))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + cityURL(fs, "/session/") + info.ID + "/terminal"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if got := readTerminalUntil(t, conn, "terminal ready"); !strings.Contains(got, "terminal ready") {
		t.Fatalf("initial terminal output = %q, want terminal ready", got)
	}

	if err := conn.WriteJSON(terminalClientFrame{Type: "resize", Cols: 101, Rows: 33}); err != nil {
		t.Fatalf("WriteJSON resize: %v", err)
	}
	select {
	case dims := <-provider.resizeCh:
		if dims != [2]int{101, 33} {
			t.Fatalf("resize dims = %#v, want 101x33", dims)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resize")
	}

	if err := conn.WriteJSON(terminalClientFrame{Type: "input", Data: "ping\n"}); err != nil {
		t.Fatalf("WriteJSON input: %v", err)
	}
	if got := readTerminalUntil(t, conn, "echo:ping"); !strings.Contains(got, "echo:ping") {
		t.Fatalf("terminal output after input = %q, want echo:ping", got)
	}
}

func TestHandleSessionTerminalRejectsUnsupportedProvider(t *testing.T) {
	fs := newSessionFakeState(t)
	info := createTestSession(t, fs.cityBeadStore, fs.sp, "No Terminal")
	req := httptest.NewRequest(http.MethodGet, cityURL(fs, "/session/")+info.ID+"/terminal", nil)
	rec := httptest.NewRecorder()

	newTestCityHandler(t, fs).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "terminal_attach_unsupported") {
		t.Fatalf("body = %s, want terminal_attach_unsupported", rec.Body.String())
	}
}

func TestCheckSessionTerminalOriginAllowsForwardedDashboardHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://100.96.12.40:8372/v0/city/gr7n-city/session/gc-1/terminal", nil)
	req.Header.Set("Origin", "https://city.gr7n.com")
	req.Header.Set("X-Forwarded-Host", "city.gr7n.com")

	if !checkSessionTerminalOrigin(req) {
		t.Fatal("origin check rejected dashboard origin matching X-Forwarded-Host")
	}
}

func TestCheckSessionTerminalOriginRejectsMismatchedForwardedHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://100.96.12.40:8372/v0/city/gr7n-city/session/gc-1/terminal", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("X-Forwarded-Host", "city.gr7n.com")

	if checkSessionTerminalOrigin(req) {
		t.Fatal("origin check accepted mismatched origin")
	}
}

func readTerminalUntil(t *testing.T, conn *websocket.Conn, needle string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var seen strings.Builder
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			if strings.Contains(err.Error(), "i/o timeout") {
				continue
			}
			t.Fatalf("ReadMessage: %v", err)
		}
		seen.Write(payload)
		if strings.Contains(seen.String(), needle) {
			return seen.String()
		}
	}
	t.Fatalf("timed out waiting for %q; saw %q", needle, seen.String())
	return seen.String()
}
