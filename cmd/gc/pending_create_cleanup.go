package main

import (
	"fmt"
	"io"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
	sessionpkg "github.com/gastownhall/gascity/internal/session"
)

func clearCompletedPendingCreateClaim(session *beads.Bead, store beads.Store) (bool, error) {
	if session == nil || store == nil || !sessionpkg.PendingCreateClaimCompletedAttempt(session.Metadata) {
		return false, nil
	}
	patch := map[string]string{
		"pending_create_claim":      "",
		"pending_create_started_at": "",
		"last_woke_at":              "",
	}
	if err := store.SetMetadataBatch(session.ID, patch); err != nil {
		return false, err
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]string, len(patch))
	}
	for key, value := range patch {
		session.Metadata[key] = value
	}
	return true, nil
}

func annotateEmptyWake(store beads.Store, session *beads.Bead, now time.Time, stderr io.Writer) {
	if store == nil || session == nil || session.ID == "" {
		return
	}
	patch := make(map[string]string, 6)
	annotateEmptyWakePatch(patch, session.Metadata, now)
	if err := store.SetMetadataBatch(session.ID, patch); err != nil {
		if stderr != nil {
			fmt.Fprintf(stderr, "session reconciler: recording empty wake %s: %v\n", session.Metadata["session_name"], err) //nolint:errcheck
		}
		return
	}
	if session.Metadata == nil {
		session.Metadata = make(map[string]string, len(patch))
	}
	for key, value := range patch {
		session.Metadata[key] = value
	}
}
