package events_test

import (
	"path/filepath"
	"testing"

	"lease-broker-successor/internal/core"
	"lease-broker-successor/internal/events"
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
