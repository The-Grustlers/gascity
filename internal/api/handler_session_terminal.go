package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/session"
	"github.com/gorilla/websocket"
)

const terminalResizeTimeout = 2 * time.Second

type terminalClientFrame struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type terminalServerFrame struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
}

type terminalWebSocketWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

var sessionTerminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin:     checkSessionTerminalOrigin,
}

func checkSessionTerminalOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := strings.TrimSpace(parsed.Host)
	if originHost == "" {
		return false
	}
	if equalHost(originHost, r.Host) {
		return true
	}
	for _, forwardedHost := range r.Header.Values("X-Forwarded-Host") {
		for _, host := range strings.Split(forwardedHost, ",") {
			if equalHost(originHost, strings.TrimSpace(host)) {
				return true
			}
		}
	}
	return false
}

func equalHost(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	return a != "" && b != "" && a == b
}

func parseCitySessionTerminalPath(path string) (string, string, bool) {
	rest, ok := strings.CutPrefix(path, "/v0/city/")
	if !ok {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 4 || parts[1] != "session" || parts[3] != "terminal" {
		return "", "", false
	}
	cityName, err := url.PathUnescape(parts[0])
	if err != nil || cityName == "" {
		return "", "", false
	}
	sessionID, err := url.PathUnescape(parts[2])
	if err != nil || sessionID == "" {
		return "", "", false
	}
	return cityName, sessionID, true
}

func (s *Server) handleSessionTerminal(w http.ResponseWriter, r *http.Request, target string) {
	store := s.state.CityBeadStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "session_store_unavailable", "session store unavailable")
		return
	}

	id, err := s.resolveSessionIDAllowClosedWithConfig(store, target)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "session_resolution_failed", err.Error())
		return
	}
	info, err := s.sessionManager(store).Get(id)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "session_lookup_failed", err.Error())
		return
	}
	if info.Closed || (info.State != session.StateActive && info.State != session.StateAwake) {
		writeError(w, http.StatusConflict, "session_not_active", "session is not active")
		return
	}
	if strings.TrimSpace(info.SessionName) == "" {
		writeError(w, http.StatusConflict, "session_runtime_missing", "session runtime name is missing")
		return
	}

	sp := s.state.SessionProvider()
	if sp == nil {
		writeError(w, http.StatusServiceUnavailable, "session_provider_unavailable", "session provider unavailable")
		return
	}
	if !sp.IsRunning(info.SessionName) {
		writeError(w, http.StatusConflict, "session_not_running", "session runtime is not running")
		return
	}
	terminalProvider, ok := sp.(runtime.TerminalAttachSpecProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "terminal_attach_unsupported", "session provider does not support browser terminal attach")
		return
	}
	attach, err := terminalProvider.TerminalAttachCommand(info.SessionName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "terminal_attach_unavailable", err.Error())
		return
	}
	if strings.TrimSpace(attach.Path) == "" {
		writeError(w, http.StatusNotImplemented, "terminal_attach_unsupported", "session provider did not return an attach command")
		return
	}

	conn, err := sessionTerminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	runSessionTerminalBridge(r.Context(), conn, terminalProvider, info.SessionName, attach)
}

func runSessionTerminalBridge(
	ctx context.Context,
	conn *websocket.Conn,
	provider runtime.TerminalAttachSpecProvider,
	sessionName string,
	attach runtime.TerminalCommandSpec,
) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = conn.Close() }()

	writer := &terminalWebSocketWriter{conn: conn}
	cmd := exec.CommandContext(ctx, attach.Path, attach.Args...)
	cmd.Env = append(os.Environ(), attach.Env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = writer.writeControl("error", fmt.Sprintf("opening terminal input: %v", err))
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = writer.writeControl("error", fmt.Sprintf("opening terminal output: %v", err))
		_ = stdin.Close()
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = writer.writeControl("error", fmt.Sprintf("opening terminal errors: %v", err))
		_ = stdin.Close()
		return
	}
	if err := cmd.Start(); err != nil {
		_ = writer.writeControl("error", fmt.Sprintf("starting terminal attach: %v", err))
		_ = stdin.Close()
		return
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	go copyTerminalOutput(ctx, cancel, writer, stdout)
	go copyTerminalOutput(ctx, cancel, writer, stderr)
	go func() {
		err := <-done
		if ctx.Err() == nil && err != nil {
			_ = writer.writeControl("error", fmt.Sprintf("terminal attach exited: %v", err))
		}
		_ = writer.close(websocket.CloseNormalClosure, "terminal attach closed")
		_ = conn.Close()
		cancel()
	}()

	_ = writer.writeControl("ready", "")
	conn.SetReadLimit(1 << 20)
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			cancel()
			_ = stdin.Close()
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var frame terminalClientFrame
		if err := json.Unmarshal(payload, &frame); err != nil {
			_ = writer.writeControl("error", "invalid terminal control frame")
			continue
		}
		switch frame.Type {
		case "input":
			if frame.Data == "" {
				continue
			}
			if _, err := io.WriteString(stdin, frame.Data); err != nil {
				_ = writer.writeControl("error", fmt.Sprintf("writing terminal input: %v", err))
				cancel()
				_ = stdin.Close()
				return
			}
		case "resize":
			runTerminalResizeCommand(ctx, provider, sessionName, frame.Cols, frame.Rows)
		default:
			_ = writer.writeControl("error", "unknown terminal control frame: "+frame.Type)
		}
	}
}

func copyTerminalOutput(ctx context.Context, cancel context.CancelFunc, writer *terminalWebSocketWriter, src io.Reader) {
	buf := make([]byte, 8192)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if writeErr := writer.writeBinary(buf[:n]); writeErr != nil {
				cancel()
				return
			}
		}
		if err != nil {
			if err != io.EOF && ctx.Err() == nil {
				_ = writer.writeControl("error", fmt.Sprintf("reading terminal output: %v", err))
			}
			return
		}
	}
}

func runTerminalResizeCommand(ctx context.Context, provider runtime.TerminalAttachSpecProvider, sessionName string, cols, rows int) {
	if cols < 2 || rows < 2 {
		return
	}
	resize, err := provider.TerminalResizeCommand(sessionName, cols, rows)
	if err != nil || strings.TrimSpace(resize.Path) == "" {
		return
	}
	resizeCtx, cancel := context.WithTimeout(ctx, terminalResizeTimeout)
	defer cancel()
	cmd := exec.CommandContext(resizeCtx, resize.Path, resize.Args...)
	cmd.Env = append(os.Environ(), resize.Env...)
	_ = cmd.Run()
}

func (w *terminalWebSocketWriter) writeBinary(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, payload)
}

func (w *terminalWebSocketWriter) writeControl(frameType, data string) error {
	payload, err := json.Marshal(terminalServerFrame{Type: frameType, Data: data})
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, payload)
}

func (w *terminalWebSocketWriter) close(code int, text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, text),
		time.Now().Add(time.Second),
	)
}
