package main

import (
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
