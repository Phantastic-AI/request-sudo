package twilioadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"request-sudo/internal/events"
)

// CanonTailer reads the broker's append-only event log and surfaces
// new `request_created` events. The adapter is a passive read-only
// observer; the single-writer rule (only request-sudod writes canon)
// is preserved.
//
// Implementation: read-then-poll. On startup, scan the entire file
// (skipping already-processed events stored in-memory). Then poll the
// file every 200ms for new lines. This is not as efficient as inotify
// but is portable and adequate for sudo-request volumes (a few per day).
type CanonTailer struct {
	path     string
	mu       sync.Mutex
	offset   int64
	seen     map[string]bool
	interval time.Duration
}

// NewCanonTailer constructs a tailer pointed at the broker's event log.
func NewCanonTailer(path string) *CanonTailer {
	return &CanonTailer{
		path:     path,
		seen:     map[string]bool{},
		interval: 200 * time.Millisecond,
	}
}

// PendingRequest is the minimal shape the adapter needs to route SMS.
type PendingRequest struct {
	RequestID     string
	Requester     string // username or "uid:N"
	Argv          []string
	Reason        string
	CreatedAt     time.Time
}

// Run loops until ctx is canceled. On each new request_created event,
// calls handler(event). Handler errors are logged via logErr and do
// not abort the loop.
func (t *CanonTailer) Run(ctx context.Context, handler func(PendingRequest), logErr func(error)) error {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	// initial scan
	if err := t.scanOnce(handler, logErr); err != nil {
		logErr(fmt.Errorf("initial canon scan: %w", err))
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := t.scanOnce(handler, logErr); err != nil {
				logErr(fmt.Errorf("canon scan: %w", err))
			}
		}
	}
}

func (t *CanonTailer) scanOnce(handler func(PendingRequest), logErr func(error)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	file, err := os.Open(t.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			// trim trailing newline
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				if err := t.handleLine(line, handler); err != nil {
					logErr(fmt.Errorf("handle canon line: %w", err))
				}
			}
			t.offset += int64(len(line)) + 1
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *CanonTailer) handleLine(line []byte, handler func(PendingRequest)) error {
	var event events.Event
	if err := json.Unmarshal(line, &event); err != nil {
		return fmt.Errorf("decode event: %w", err)
	}
	if event.Type != events.TypeRequestCreated {
		return nil
	}
	if t.seen[event.RequestID] {
		return nil
	}
	t.seen[event.RequestID] = true

	var details events.RequestCreatedDetails
	if err := json.Unmarshal(event.Details, &details); err != nil {
		return fmt.Errorf("decode request_created details: %w", err)
	}
	req := details.Request
	requester := req.Requester.Username
	if requester == "" {
		requester = fmt.Sprintf("uid:%d", req.Requester.UID)
	}
	pending := PendingRequest{
		RequestID: req.ID,
		Requester: requester,
		Argv:      append([]string(nil), req.Argv...),
		Reason:    req.Reason,
		CreatedAt: req.CreatedAt,
	}
	handler(pending)
	return nil
}
