// Restart-mid-Verify recovery regression test (US-009 in .omc/prd.json).
//
// Proves the liveness clause in ADR-0005a T34: a pending slot in the
// adapter's in-memory PendingSet survives adapter restart by replaying
// the adapter-owned audit JSONL, AND auto-releases when its TTL
// elapses. This test is in-package because it needs to inject the
// unexported `now` field to advance simulated time past TTL.
package twilioadapter

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRestart_PendingSurvivesAndExpiresAfterTTL(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "twilio-audit.jsonl")

	audit, err := NewAuditLog(auditPath)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}

	t0 := time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC)
	pset1 := NewPendingSet(10*time.Minute, audit)
	pset1.now = func() time.Time { return t0 }

	// Simulate: adapter receives request_created for req_A, opens the slot.
	if err := pset1.OpenPending("+13102957704", "req_A"); err != nil {
		t.Fatalf("OpenPending: %v", err)
	}
	// Confirm slot held.
	if rid, ok := pset1.Resolve("+13102957704"); !ok || rid != "req_A" {
		t.Fatalf("Resolve before restart: ok=%v rid=%q (want ok=true rid=req_A)", ok, rid)
	}

	// (AuditLog Write opens/closes per-call, so all rows are already
	// durable on disk — no Close needed to simulate the restart.)

	// --- Simulated adapter restart: fresh AuditLog + fresh PendingSet ---
	audit2, err := NewAuditLog(auditPath)
	if err != nil {
		t.Fatalf("audit reopen: %v", err)
	}
	_ = audit2

	pset2 := NewPendingSet(10*time.Minute, audit2)
	// Pin clock to t0 first (replay logic should use timestamps from
	// the audit, not the current clock, when reconstructing state).
	pset2.now = func() time.Time { return t0 }
	if err := pset2.ReplayFromAudit(auditPath); err != nil {
		t.Fatalf("ReplayFromAudit: %v", err)
	}

	// State survived restart: slot still held for the same phone.
	if rid, ok := pset2.Resolve("+13102957704"); !ok || rid != "req_A" {
		t.Fatalf("Resolve after restart: ok=%v rid=%q (want ok=true rid=req_A)", ok, rid)
	}
	// Second OpenPending for same phone still blocked.
	if err := pset2.OpenPending("+13102957704", "req_B"); err == nil {
		t.Fatalf("OpenPending same phone post-restart should fail with ErrPhoneBusy; got nil")
	}

	// --- Advance clock past TTL → lazy expiry on next Resolve ---
	pset2.now = func() time.Time { return t0.Add(11 * time.Minute) }
	if rid, ok := pset2.Resolve("+13102957704"); ok {
		t.Fatalf("Resolve after TTL expiry: ok=%v rid=%q (want ok=false)", ok, rid)
	}
	// Now OpenPending for a NEW request on the same phone succeeds.
	if err := pset2.OpenPending("+13102957704", "req_B"); err != nil {
		t.Fatalf("OpenPending after TTL expiry: %v", err)
	}
}

func TestRestart_ExpireDueScansLazyEntries(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "twilio-audit.jsonl")
	audit, err := NewAuditLog(auditPath)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}

	t0 := time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC)
	pset := NewPendingSet(10*time.Minute, audit)
	pset.now = func() time.Time { return t0 }

	for _, pair := range []struct {
		phone, rid string
	}{
		{"+13105550101", "req_1"},
		{"+13105550102", "req_2"},
		{"+13105550103", "req_3"},
	} {
		if err := pset.OpenPending(pair.phone, pair.rid); err != nil {
			t.Fatalf("OpenPending %s: %v", pair.phone, err)
		}
	}

	// Advance past TTL — all three should be eligible for expiry.
	pset.now = func() time.Time { return t0.Add(11 * time.Minute) }
	released := pset.ExpireDue()
	if released != 3 {
		t.Fatalf("ExpireDue released=%d, want 3", released)
	}
	// And subsequent OpenPending for any of them succeeds.
	if err := pset.OpenPending("+13105550101", "req_1b"); err != nil {
		t.Fatalf("post-ExpireDue OpenPending: %v", err)
	}
}
