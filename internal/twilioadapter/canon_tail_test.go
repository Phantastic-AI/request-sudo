package twilioadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"request-sudo/internal/core"
	"request-sudo/internal/events"
)

// CanonTailer is the bridge between broker canon and adapter routing.
// These tests exercise the tail-and-dedup behavior offline (no broker).

func TestCanonTailer_PicksUpRequestCreated(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	log, err := events.NewLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// Append a synthetic request_created event.
	details, err := events.MarshalDetails(events.RequestCreatedDetails{Request: core.Request{
		ID:        "req_abc",
		Argv:      []string{"/bin/echo", "hello"},
		Reason:    "test",
		Requester: core.PeerIdentity{UID: 1000, Username: "aditya"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append(events.Event{
		RequestID: "req_abc",
		Actor:     core.Actor{Kind: "requester", ID: "aditya"},
		Type:      events.TypeRequestCreated,
		Details:   details,
	}); err != nil {
		t.Fatal(err)
	}

	tailer := NewCanonTailer(logPath)
	tailer.interval = 20 * time.Millisecond

	var (
		mu      sync.Mutex
		picked  []PendingRequest
	)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = tailer.Run(ctx, func(p PendingRequest) {
			mu.Lock()
			picked = append(picked, p)
			mu.Unlock()
		}, func(err error) { t.Logf("tail err: %v", err) })
		close(done)
	}()

	// Wait briefly for the initial scan to fire.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(picked)
		mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(picked) != 1 {
		t.Fatalf("expected 1 picked request, got %d", len(picked))
	}
	if picked[0].RequestID != "req_abc" || picked[0].Requester != "aditya" {
		t.Fatalf("unexpected: %+v", picked[0])
	}
}

func TestCanonTailer_IgnoresNonRequestCreated(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	// Write a raw non-request_created event line so we don't have to
	// stand up a broker.
	event := events.Event{
		EventID:   "evt_test",
		Hash:      "deadbeef",
		RequestID: "req_xyz",
		Timestamp: time.Now().UTC(),
		Actor:     core.Actor{Kind: "broker", ID: "request-sudod"},
		Type:      events.TypeExecutionSucceeded,
	}
	line, _ := json.Marshal(event)
	if err := os.WriteFile(logPath, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	tailer := NewCanonTailer(logPath)
	tailer.interval = 20 * time.Millisecond

	var picked int
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = tailer.Run(ctx, func(p PendingRequest) { picked++ }, func(err error) {})
		close(done)
	}()
	<-done
	if picked != 0 {
		t.Fatalf("expected 0 picked, got %d", picked)
	}
}
