package api

import (
	"encoding/json"
	"testing"
)

func TestBeadEventPayloadUnmarshalAcceptsRawHookBead(t *testing.T) {
	var payload BeadEventPayload
	if err := json.Unmarshal([]byte(`{
		"id":"gc-123",
		"title":"mayor",
		"status":"open",
		"issue_type":"session",
		"labels":["gc:session"]
	}`), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Bead.ID != "gc-123" {
		t.Fatalf("bead.id = %q, want gc-123", payload.Bead.ID)
	}
	if payload.Bead.Type != "session" {
		t.Fatalf("bead.issue_type = %q, want session", payload.Bead.Type)
	}
}

func TestBeadEventPayloadUnmarshalAcceptsWrappedBead(t *testing.T) {
	var payload BeadEventPayload
	if err := json.Unmarshal([]byte(`{
		"bead":{
			"id":"gc-456",
			"title":"mail",
			"status":"open",
			"issue_type":"message"
		}
	}`), &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if payload.Bead.ID != "gc-456" {
		t.Fatalf("bead.id = %q, want gc-456", payload.Bead.ID)
	}
	if payload.Bead.Type != "message" {
		t.Fatalf("bead.issue_type = %q, want message", payload.Bead.Type)
	}
}
