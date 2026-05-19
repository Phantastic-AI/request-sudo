package twilioadapter

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

// ErrPhoneBusy is returned by OpenPending when the recipient phone
// already has a non-expired pending verification entry.
var ErrPhoneBusy = errors.New("twilio: phone has a pending verification within TTL")

// Audit event type discriminators for the adapter-owned audit log.
// These events are NEVER written to the broker canon (single-writer
// rule preserved); they live only in the adapter's own JSONL audit.
const (
	auditTypeVerifyPendingOpened   = "verify_pending_opened"
	auditTypeVerifyPendingReleased = "verify_pending_released"
	auditTypeVerifyPendingExpired  = "verify_pending_expired"
)

// pendingEntry tracks a single in-flight Verify per recipient phone.
type pendingEntry struct {
	RequestID string
	OpenedAt  time.Time
}

// PendingSet enforces "at most one pending Verify per recipient phone
// within TTL". State survives adapter restart by replaying the adapter
// audit JSONL (verify_pending_opened / released / expired events).
//
// Thread-safe. All public methods take the internal mutex.
type PendingSet struct {
	mu    sync.Mutex
	ttl   time.Duration
	now   func() time.Time // injectable for testing
	audit *AuditLog        // best-effort emitter; may be nil in tests
	items map[string]pendingEntry
}

// NewPendingSet constructs a PendingSet with the given TTL and audit
// sink. The audit sink may be nil (audit emission becomes a no-op).
func NewPendingSet(ttl time.Duration, audit *AuditLog) *PendingSet {
	return &PendingSet{
		ttl:   ttl,
		now:   time.Now,
		audit: audit,
		items: map[string]pendingEntry{},
	}
}

// OpenPending tries to claim phone for requestID. Returns ErrPhoneBusy
// if a non-expired entry already exists for the same phone. If an
// existing entry is past TTL, it is lazily released (emit
// verify_pending_expired) and the new claim succeeds.
func (ps *PendingSet) OpenPending(phone, requestID string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := ps.now()
	if existing, ok := ps.items[phone]; ok {
		if now.Sub(existing.OpenedAt) < ps.ttl {
			return ErrPhoneBusy
		}
		// expired: lazily release and emit expired event
		delete(ps.items, phone)
		ps.emit(auditTypeVerifyPendingExpired, existing.RequestID, phone, now)
	}
	ps.items[phone] = pendingEntry{RequestID: requestID, OpenedAt: now}
	ps.emit(auditTypeVerifyPendingOpened, requestID, phone, now)
	return nil
}

// Resolve returns the request_id pending for phone (if any non-expired).
// Lazy TTL-expire: if the entry is past TTL, release it (emit
// verify_pending_expired) and return "", false.
func (ps *PendingSet) Resolve(phone string) (string, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	entry, ok := ps.items[phone]
	if !ok {
		return "", false
	}
	now := ps.now()
	if now.Sub(entry.OpenedAt) >= ps.ttl {
		delete(ps.items, phone)
		ps.emit(auditTypeVerifyPendingExpired, entry.RequestID, phone, now)
		return "", false
	}
	return entry.RequestID, true
}

// Release explicitly clears the slot (called after broker review.approve
// or .deny succeeded). Emits verify_pending_released. No-op if no entry.
func (ps *PendingSet) Release(phone string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	entry, ok := ps.items[phone]
	if !ok {
		return
	}
	delete(ps.items, phone)
	ps.emit(auditTypeVerifyPendingReleased, entry.RequestID, phone, ps.now())
}

// ExpireDue scans entries and lazily releases any past TTL. Returns the
// number of entries released. Tests drive this directly to verify TTL
// behavior without sleeping.
func (ps *PendingSet) ExpireDue() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	now := ps.now()
	count := 0
	for phone, entry := range ps.items {
		if now.Sub(entry.OpenedAt) >= ps.ttl {
			delete(ps.items, phone)
			ps.emit(auditTypeVerifyPendingExpired, entry.RequestID, phone, now)
			count++
		}
	}
	return count
}

// emit writes a best-effort audit row. Audit-write errors are logged
// via stdlib log but never bubble up.
func (ps *PendingSet) emit(typ, requestID, phone string, ts time.Time) {
	if ps.audit == nil {
		return
	}
	row := map[string]any{
		"type":             typ,
		"request_id":       requestID,
		"recipient_masked": MaskPhone(phone),
		"recipient_phone":  phone,
		"wall_ts":          ts.UTC().Format(time.RFC3339Nano),
	}
	if err := ps.audit.Write(row); err != nil {
		log.Printf("pending_set: audit write %s: %v", typ, err)
	}
}

// auditRow is the minimal shape of a verify_pending_* row used during
// replay. Extra fields are ignored.
type auditRow struct {
	Type           string `json:"type"`
	RequestID      string `json:"request_id"`
	RecipientPhone string `json:"recipient_phone"`
	WallTS         string `json:"wall_ts"`
}

// ReplayFromAudit scans the adapter-owned audit JSONL for
// verify_pending_* events and applies them in timestamp order. After
// replay, the in-memory map matches the pre-restart state, minus any
// entries that have since expired (those are lazily dropped via
// ExpireDue at the end).
//
// The audit log is the adapter's own append-only sink (NOT broker
// canon — the broker is single-writer). Missing file is a no-op.
func (ps *PendingSet) ReplayFromAudit(adapterAuditPath string) error {
	file, err := os.Open(adapterAuditPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	type parsed struct {
		row auditRow
		ts  time.Time
	}
	var rows []parsed
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 {
				var row auditRow
				if err := json.Unmarshal(line, &row); err != nil {
					// malformed: skip
					if readErr == io.EOF {
						break
					}
					continue
				}
				switch row.Type {
				case auditTypeVerifyPendingOpened,
					auditTypeVerifyPendingReleased,
					auditTypeVerifyPendingExpired:
					// only these events affect the set
				default:
					if readErr == io.EOF {
						break
					}
					continue
				}
				if row.RecipientPhone == "" || row.RequestID == "" {
					if readErr == io.EOF {
						break
					}
					continue
				}
				ts, terr := time.Parse(time.RFC3339Nano, row.WallTS)
				if terr != nil {
					if readErr == io.EOF {
						break
					}
					continue
				}
				rows = append(rows, parsed{row: row, ts: ts})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	// stable sort by timestamp
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].ts.Before(rows[j].ts)
	})

	ps.mu.Lock()
	defer ps.mu.Unlock()
	for _, p := range rows {
		switch p.row.Type {
		case auditTypeVerifyPendingOpened:
			ps.items[p.row.RecipientPhone] = pendingEntry{
				RequestID: p.row.RequestID,
				OpenedAt:  p.ts,
			}
		case auditTypeVerifyPendingReleased, auditTypeVerifyPendingExpired:
			if existing, ok := ps.items[p.row.RecipientPhone]; ok && existing.RequestID == p.row.RequestID {
				delete(ps.items, p.row.RecipientPhone)
			}
		}
	}
	return nil
}
