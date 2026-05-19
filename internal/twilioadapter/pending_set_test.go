package twilioadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestAudit returns a fresh AuditLog under t.TempDir() and the path.
func newTestAudit(t *testing.T) (*AuditLog, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio-audit.jsonl")
	a, err := NewAuditLog(path)
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}
	return a, path
}

// readAuditLines reads each non-empty JSON line of the audit file.
func readAuditLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var rows []map[string]any
	for _, line := range splitNonEmpty(raw) {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("decode audit line %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func splitNonEmpty(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			if i > start {
				out = append(out, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

func countType(rows []map[string]any, typ string) int {
	n := 0
	for _, r := range rows {
		if r["type"] == typ {
			n++
		}
	}
	return n
}

// 1. Happy path: open, resolve, release, resolve.
func TestPendingSet_HappyPath(t *testing.T) {
	audit, path := newTestAudit(t)
	ps := NewPendingSet(10*time.Minute, audit)

	if err := ps.OpenPending("+15551234567", "req_A"); err != nil {
		t.Fatalf("OpenPending: %v", err)
	}
	id, ok := ps.Resolve("+15551234567")
	if !ok || id != "req_A" {
		t.Fatalf("Resolve = (%q,%v), want (req_A,true)", id, ok)
	}
	ps.Release("+15551234567")
	if id, ok := ps.Resolve("+15551234567"); ok {
		t.Fatalf("Resolve after Release = (%q,%v), want empty,false", id, ok)
	}

	rows := readAuditLines(t, path)
	if got := countType(rows, auditTypeVerifyPendingOpened); got != 1 {
		t.Errorf("opened rows = %d, want 1", got)
	}
	if got := countType(rows, auditTypeVerifyPendingReleased); got != 1 {
		t.Errorf("released rows = %d, want 1", got)
	}
}

// 2. Same phone within TTL: ErrPhoneBusy.
func TestPendingSet_SamePhoneBusy(t *testing.T) {
	audit, _ := newTestAudit(t)
	ps := NewPendingSet(10*time.Minute, audit)

	if err := ps.OpenPending("+15551234567", "req_A"); err != nil {
		t.Fatalf("first OpenPending: %v", err)
	}
	if err := ps.OpenPending("+15551234567", "req_B"); err != ErrPhoneBusy {
		t.Fatalf("second OpenPending err = %v, want ErrPhoneBusy", err)
	}
	id, ok := ps.Resolve("+15551234567")
	if !ok || id != "req_A" {
		t.Fatalf("Resolve = (%q,%v), want (req_A,true)", id, ok)
	}
}

// 3. TTL expiry — lazy via Resolve.
func TestPendingSet_TTLExpiry_Resolve(t *testing.T) {
	audit, path := newTestAudit(t)
	ps := NewPendingSet(10*time.Minute, audit)

	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	ps.now = func() time.Time { return t0 }
	if err := ps.OpenPending("+15551234567", "req_A"); err != nil {
		t.Fatalf("OpenPending: %v", err)
	}
	// Advance clock past TTL.
	ps.now = func() time.Time { return t0.Add(11 * time.Minute) }
	id, ok := ps.Resolve("+15551234567")
	if ok {
		t.Fatalf("Resolve after TTL = (%q,%v), want empty,false", id, ok)
	}
	rows := readAuditLines(t, path)
	if got := countType(rows, auditTypeVerifyPendingExpired); got != 1 {
		t.Errorf("expired rows = %d, want 1", got)
	}
}

// 4. TTL expiry — second claim succeeds after expiry.
func TestPendingSet_ClaimAfterExpiry(t *testing.T) {
	audit, _ := newTestAudit(t)
	ps := NewPendingSet(10*time.Minute, audit)

	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	ps.now = func() time.Time { return t0 }
	if err := ps.OpenPending("+15551234567", "req_A"); err != nil {
		t.Fatalf("OpenPending A: %v", err)
	}
	ps.now = func() time.Time { return t0.Add(11 * time.Minute) }
	if err := ps.OpenPending("+15551234567", "req_B"); err != nil {
		t.Fatalf("OpenPending B after expiry: %v", err)
	}
	id, ok := ps.Resolve("+15551234567")
	if !ok || id != "req_B" {
		t.Fatalf("Resolve = (%q,%v), want (req_B,true)", id, ok)
	}
}

// 5. ExpireDue: scan + release across multiple phones.
func TestPendingSet_ExpireDue(t *testing.T) {
	audit, _ := newTestAudit(t)
	ps := NewPendingSet(10*time.Minute, audit)

	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	ps.now = func() time.Time { return t0 }
	if err := ps.OpenPending("+15550001111", "req_A"); err != nil {
		t.Fatal(err)
	}
	ps.now = func() time.Time { return t0.Add(2 * time.Minute) }
	if err := ps.OpenPending("+15550002222", "req_B"); err != nil {
		t.Fatal(err)
	}
	// Advance only past A's TTL (11m), not past B's (which expires at 12m).
	ps.now = func() time.Time { return t0.Add(11 * time.Minute) }
	if got := ps.ExpireDue(); got != 1 {
		t.Fatalf("ExpireDue first = %d, want 1", got)
	}
	// Advance past B's TTL.
	ps.now = func() time.Time { return t0.Add(13 * time.Minute) }
	if got := ps.ExpireDue(); got != 1 {
		t.Fatalf("ExpireDue second = %d, want 1", got)
	}
	// Nothing left.
	if got := ps.ExpireDue(); got != 0 {
		t.Fatalf("ExpireDue third = %d, want 0", got)
	}
}

// 6. Replay from audit: opened, released, opened (same phone, new id).
func TestPendingSet_ReplayFromAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio-audit.jsonl")
	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	lines := []map[string]any{
		{
			"type":             auditTypeVerifyPendingOpened,
			"request_id":       "req_A",
			"recipient_masked": MaskPhone("+15551234"),
			"recipient_phone":  "+15551234",
			"wall_ts":          t0.Format(time.RFC3339Nano),
		},
		{
			"type":             auditTypeVerifyPendingReleased,
			"request_id":       "req_A",
			"recipient_masked": MaskPhone("+15551234"),
			"recipient_phone":  "+15551234",
			"wall_ts":          t0.Add(5 * time.Minute).Format(time.RFC3339Nano),
		},
		{
			"type":             auditTypeVerifyPendingOpened,
			"request_id":       "req_B",
			"recipient_masked": MaskPhone("+15551234"),
			"recipient_phone":  "+15551234",
			"wall_ts":          t0.Add(10 * time.Minute).Format(time.RFC3339Nano),
		},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		b, _ := json.Marshal(l)
		f.Write(append(b, '\n'))
	}
	f.Close()

	ps := NewPendingSet(10*time.Minute, nil)
	if err := ps.ReplayFromAudit(path); err != nil {
		t.Fatalf("ReplayFromAudit: %v", err)
	}
	// Set clock to t0 + 11m. req_B was opened at t0+10m, so TTL not yet expired.
	ps.now = func() time.Time { return t0.Add(11 * time.Minute) }
	id, ok := ps.Resolve("+15551234")
	if !ok || id != "req_B" {
		t.Fatalf("Resolve after replay = (%q,%v), want (req_B,true)", id, ok)
	}
}

// 7. Replay tolerates malformed lines.
func TestPendingSet_ReplayMalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio-audit.jsonl")
	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// malformed
	f.WriteString("{\n")
	// good
	good := map[string]any{
		"type":             auditTypeVerifyPendingOpened,
		"request_id":       "req_A",
		"recipient_masked": MaskPhone("+15559998888"),
		"recipient_phone":  "+15559998888",
		"wall_ts":          t0.Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(good)
	f.Write(append(b, '\n'))
	// empty-object (missing required fields)
	f.WriteString("{}\n")
	f.Close()

	ps := NewPendingSet(10*time.Minute, nil)
	if err := ps.ReplayFromAudit(path); err != nil {
		t.Fatalf("ReplayFromAudit: %v", err)
	}
	ps.now = func() time.Time { return t0.Add(1 * time.Minute) }
	id, ok := ps.Resolve("+15559998888")
	if !ok || id != "req_A" {
		t.Fatalf("Resolve after malformed-tolerant replay = (%q,%v), want (req_A,true)", id, ok)
	}
}

// 8. Replay tolerates missing audit file.
func TestPendingSet_ReplayMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.jsonl")
	ps := NewPendingSet(10*time.Minute, nil)
	if err := ps.ReplayFromAudit(path); err != nil {
		t.Fatalf("ReplayFromAudit missing = %v, want nil", err)
	}
	if _, ok := ps.Resolve("+15550000000"); ok {
		t.Fatalf("Resolve on empty set returned ok")
	}
}
