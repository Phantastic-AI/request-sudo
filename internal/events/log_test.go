package events_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"request-sudo/internal/core"
	"request-sudo/internal/events"
)

func TestLogAppendAndReplayMaintainsHashChain(t *testing.T) {
	log, err := events.NewLog(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	createdDetails, _ := events.MarshalDetails(events.RequestCreatedDetails{Request: core.Request{ID: "req_1"}})
	first, err := log.Append(events.Event{RequestID: "req_1", Actor: core.Actor{Kind: "requester", ID: "alice"}, Type: events.TypeRequestCreated, Details: createdDetails})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	approvedDetails, _ := events.MarshalDetails(events.ApprovalDetails{Approver: core.Actor{Kind: "local", ID: "root"}})
	second, err := log.Append(events.Event{RequestID: "req_1", Actor: core.Actor{Kind: "local", ID: "root"}, Type: events.TypeRequestApproved, Details: approvedDetails})
	if err != nil {
		t.Fatalf("append second: %v", err)
	}
	if second.PrevHash != first.Hash {
		t.Fatalf("prev hash mismatch: got %q want %q", second.PrevHash, first.Hash)
	}
	history, err := log.Replay()
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("unexpected history length: %d", len(history))
	}
	if history[1].PrevHash != history[0].Hash {
		t.Fatalf("replayed chain mismatch")
	}
}

func TestReplayRejectsTamperedHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	log, err := events.NewLog(path)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	createdDetails, _ := events.MarshalDetails(events.RequestCreatedDetails{Request: core.Request{ID: "req_1"}})
	if _, err := log.Append(events.Event{RequestID: "req_1", Actor: core.Actor{Kind: "requester", ID: "alice"}, Type: events.TypeRequestCreated, Details: createdDetails}); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	replaced := bytes.Replace(data, []byte("\"hash\":\""), []byte("\"hash\":\"deadbeef"), 1)
	if err := os.WriteFile(path, replaced, 0o600); err != nil {
		t.Fatalf("write tampered: %v", err)
	}
	_, err = events.NewLog(path)
	if err == nil {
		t.Fatal("expected tamper detection error")
	}
}
