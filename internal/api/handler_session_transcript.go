package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gastownhall/gascity/internal/session"
	"github.com/gastownhall/gascity/internal/sessionlog"
	"github.com/gastownhall/gascity/internal/worker"
)

type sessionTranscriptResponse struct {
	ID         string                       `json:"id"`
	Template   string                       `json:"template"`
	Format     string                       `json:"format"`
	Turns      []outputTurn                 `json:"turns"`
	Pagination *worker.TranscriptPagination `json:"pagination,omitempty"`
}

type sessionRawTranscriptResponse struct {
	ID         string                       `json:"id"`
	Template   string                       `json:"template"`
	Format     string                       `json:"format"`
	Messages   []json.RawMessage            `json:"messages"`
	Pagination *worker.TranscriptPagination `json:"pagination,omitempty"`
}

func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request) {
	store := s.state.CityBeadStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no bead store configured")
		return
	}

	id, err := s.resolveSessionIDAllowClosedWithConfig(store, r.PathValue("id"))
	if err != nil {
		writeResolveError(w, err)
		return
	}

	catalog, err := s.workerSessionCatalog(store)
	if err != nil {
		writeSessionManagerError(w, err)
		return
	}
	info, err := catalog.Get(id)
	if err != nil {
		writeSessionManagerError(w, err)
		return
	}
	handle, err := s.workerHandleForSession(store, id)
	if err != nil {
		writeSessionManagerError(w, err)
		return
	}
	path, err := handle.TranscriptPath(r.Context())
	if err != nil && !errors.Is(err, worker.ErrHistoryUnavailable) {
		writeSessionManagerError(w, err)
		return
	}

	wantRaw := r.URL.Query().Get("format") == "raw"

	if path != "" {
		tail := 0
		if v := r.URL.Query().Get("tail"); v != "" {
			if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 {
				tail = n
			}
		}
		limit := parseSessionTranscriptLimit(r.URL.Query().Get("limit"))
		before := r.URL.Query().Get("before")
		after := r.URL.Query().Get("after")

		if before != "" && after != "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_params", "before and after are mutually exclusive")
			return
		}

		if wantRaw {
			transcript, err := handle.Transcript(r.Context(), worker.TranscriptRequest{
				TailCompactions: tail,
				BeforeEntryID:   before,
				AfterEntryID:    after,
				Raw:             true,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "reading session log: "+err.Error())
				return
			}
			writeJSON(w, http.StatusOK, sessionRawTranscriptResponse{
				ID:         info.ID,
				Template:   info.Template,
				Format:     "raw",
				Messages:   transcript.RawMessages,
				Pagination: transcript.Session.Pagination,
			})
			return
		}

		transcript, err := handle.Transcript(r.Context(), worker.TranscriptRequest{
			TailCompactions: tail,
			BeforeEntryID:   before,
			AfterEntryID:    after,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "reading session log: "+err.Error())
			return
		}
		sess := transcript.Session
		turnItems := transcriptOutputTurns(sess.Messages)
		turnItems, pagination := limitTranscriptOutputTurns(turnItems, limit, before, after, sess.Pagination, len(sess.Messages))
		turns := outputTurnsFromItems(turnItems)
		if len(turns) == 0 && before == "" && after == "" {
			if peekTurns, ok, peekErr := s.peekSessionTranscriptTurns(r.Context(), info, handle); peekErr != nil {
				writeError(w, http.StatusInternalServerError, "internal", peekErr.Error())
				return
			} else if ok {
				writeJSON(w, http.StatusOK, sessionTranscriptResponse{
					ID:       info.ID,
					Template: info.Template,
					Format:   "text",
					Turns:    peekTurns,
				})
				return
			}
		}
		writeJSON(w, http.StatusOK, sessionTranscriptResponse{
			ID:         info.ID,
			Template:   info.Template,
			Format:     "conversation",
			Turns:      turns,
			Pagination: pagination,
		})
		return
	}

	if wantRaw {
		writeJSON(w, http.StatusOK, sessionRawTranscriptResponse{
			ID:       info.ID,
			Template: info.Template,
			Format:   "raw",
			Messages: []json.RawMessage{},
		})
		return
	}

	turns, ok, peekErr := s.peekSessionTranscriptTurns(r.Context(), info, handle)
	if peekErr != nil {
		writeError(w, http.StatusInternalServerError, "internal", peekErr.Error())
		return
	}
	if ok {
		writeJSON(w, http.StatusOK, sessionTranscriptResponse{
			ID:       info.ID,
			Template: info.Template,
			Format:   "text",
			Turns:    turns,
		})
		return
	}

	writeJSON(w, http.StatusOK, sessionTranscriptResponse{
		ID:       info.ID,
		Template: info.Template,
		Format:   "conversation",
		Turns:    []outputTurn{},
	})
}

func parseSessionTranscriptLimit(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return sessionTranscriptLimit(n)
}

func sessionTranscriptLimit(n int) int {
	if n <= 0 {
		return 0
	}
	if n > maxPaginationLimit {
		return maxPaginationLimit
	}
	return n
}

type transcriptOutputTurn struct {
	turn    outputTurn
	entryID string
}

func transcriptOutputTurns(entries []*sessionlog.Entry) []transcriptOutputTurn {
	items := make([]transcriptOutputTurn, 0, len(entries))
	for _, entry := range entries {
		turn := entryToTurn(entry)
		if !outputTurnHasContent(turn) {
			continue
		}
		if len(items) > 0 && outputTurnsEquivalent(items[len(items)-1].turn, turn) {
			items[len(items)-1].turn = mergeOutputTurnMetadata(items[len(items)-1].turn, turn)
			items[len(items)-1].entryID = entry.UUID
			continue
		}
		items = append(items, transcriptOutputTurn{
			turn:    turn,
			entryID: entry.UUID,
		})
	}
	return items
}

func outputTurnsFromItems(items []transcriptOutputTurn) []outputTurn {
	turns := make([]outputTurn, 0, len(items))
	for _, item := range items {
		turns = append(turns, item.turn)
	}
	return turns
}

func limitTranscriptOutputTurns(items []transcriptOutputTurn, limit int, before, after string, existing *sessionlog.PaginationInfo, totalEntries int) ([]transcriptOutputTurn, *sessionlog.PaginationInfo) {
	if limit <= 0 {
		return items, existing
	}
	totalCount := totalEntries
	totalCompactions := 0
	hasOlderMessages := false
	if existing != nil {
		if existing.TotalMessageCount > totalCount {
			totalCount = existing.TotalMessageCount
		}
		totalCompactions = existing.TotalCompactions
		hasOlderMessages = existing.HasOlderMessages
	}

	working := items
	if before != "" {
		for i, item := range working {
			if item.entryID == before {
				working = working[:i]
				break
			}
		}
	}
	if after != "" {
		for i, item := range working {
			if item.entryID == after {
				working = working[i+1:]
				break
			}
		}
	}

	if after != "" && len(working) > limit {
		working = working[:limit]
	} else if len(working) > limit {
		working = working[len(working)-limit:]
		hasOlderMessages = true
	}

	truncatedBefore := ""
	if hasOlderMessages && len(working) > 0 {
		truncatedBefore = working[0].entryID
	}
	return working, &sessionlog.PaginationInfo{
		HasOlderMessages:       hasOlderMessages,
		TotalMessageCount:      totalCount,
		ReturnedMessageCount:   len(working),
		TruncatedBeforeMessage: truncatedBefore,
		TotalCompactions:       totalCompactions,
	}
}

func (s *Server) peekSessionTranscriptTurns(ctx context.Context, info session.Info, handle worker.Handle) ([]outputTurn, bool, error) {
	output, err := handle.Peek(ctx, 100)
	if err != nil {
		if errors.Is(err, session.ErrSessionInactive) {
			return nil, false, nil
		}
		if info.State == session.StateActive && s.sessionProviderIsRunning(info.SessionName) {
			return nil, false, err
		}
		return nil, false, nil
	}
	turns := []outputTurn{}
	if output != "" {
		turns = append(turns, outputTurn{Role: "output", Text: output})
	}
	return turns, true, nil
}

func (s *Server) sessionProviderIsRunning(sessionName string) bool {
	if s == nil || s.state == nil || s.state.SessionProvider() == nil {
		return false
	}
	return s.state.SessionProvider().IsRunning(sessionName)
}
