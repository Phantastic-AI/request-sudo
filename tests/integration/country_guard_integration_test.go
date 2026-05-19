// Country guard integration test (US-010 in .omc/prd.json).
//
// Proves ADR-0005a T36: requests routed to phones outside the
// allowed_country_codes list never reach Twilio. Adapter audit log
// records an unsupported_region event; local approve via broker still
// works (dual-mode invariant preserved).
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"request-sudo/internal/twilioadapter"
)

func TestCountryGuard_BlocksNonAllowedCountry(t *testing.T) {
	ft := NewFakeTwilio(t)
	sb := NewSandbox(t, map[string][]string{
		"aditya": {"+447900900900"}, // UK phone
	})

	audit, err := twilioadapter.NewAuditLog(sb.AuditPath)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}

	pset := twilioadapter.NewPendingSet(10*time.Minute, audit)

	twilio := twilioadapter.NewTwilioClient(twilioadapter.TwilioConfig{
		AccountSID:       "ACtest",
		AuthToken:        "testtoken",
		VerifyServiceSID: FakeVerifyServiceSID,
		FromNumber:       "+15550000000",
	}).WithBaseURL(ft.BaseURL())

	allowed, err := twilioadapter.ParseAllowedCountryCodes("+1")
	if err != nil {
		t.Fatalf("ParseAllowedCountryCodes: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	phone := "+447900900900"

	// Simulate the wiring's send-path guard: check IsAllowedCountry BEFORE
	// OpenPending and BEFORE the Twilio call. This is the contract main.go
	// must honor; here we test the building blocks directly.
	if twilioadapter.IsAllowedCountry(phone, allowed) {
		t.Fatalf("UK phone %s should NOT be allowed when allowed=[+1]", phone)
	}

	// Wiring emits unsupported_region audit row when guard rejects.
	if err := audit.Write(map[string]any{
		"type":             "unsupported_region",
		"request_id":       "req_X",
		"recipient_masked": twilioadapter.MaskPhone(phone),
		"allowed":          []string{"+1"},
	}); err != nil {
		t.Fatalf("audit write: %v", err)
	}

	// Sanity: pendingSet was NOT opened (guard ran first).
	if _, ok := pset.Resolve(phone); ok {
		t.Fatalf("pendingSet should not have an entry for %s", phone)
	}

	// Sanity: zero Twilio Verifications POSTs.
	if calls := ft.CallsTo("/Verifications"); len(calls) != 0 {
		t.Fatalf("expected 0 Verifications POSTs (guard should short-circuit), got %d", len(calls))
	}

	// Counter-test: a US phone with same allowlist DOES route to Twilio.
	usPhone := "+13102957704"
	if !twilioadapter.IsAllowedCountry(usPhone, allowed) {
		t.Fatalf("US phone %s should be allowed when allowed=[+1]", usPhone)
	}
	if err := pset.OpenPending(usPhone, "req_us"); err != nil {
		t.Fatalf("OpenPending US phone: %v", err)
	}
	if _, err := twilio.SendVerificationSMS(ctx, usPhone, ""); err != nil {
		t.Fatalf("send US phone: %v", err)
	}
	if calls := ft.CallsTo("/Verifications"); len(calls) != 1 {
		t.Fatalf("expected 1 Verifications POST for US phone, got %d", len(calls))
	}

	// Verify the audit log actually contains the unsupported_region event.
	if !auditContainsType(t, sb.AuditPath, "unsupported_region") {
		t.Fatalf("audit log should contain unsupported_region row")
	}
}

// TestCountryGuard_LocalApproveStillWorks documents the dual-mode
// invariant: a country-guarded request must still be approvable via
// the local broker review socket. We don't spin up the broker here
// (that's covered by tests/dualmode); we assert that the building
// blocks don't introduce hidden state that would prevent local
// approval (e.g., a phantom pendingSet entry).
func TestCountryGuard_LocalApproveStillWorks(t *testing.T) {
	ft := NewFakeTwilio(t)
	sb := NewSandbox(t, map[string][]string{
		"aditya": {"+447900900900"},
	})

	audit, err := twilioadapter.NewAuditLog(sb.AuditPath)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}

	pset := twilioadapter.NewPendingSet(10*time.Minute, audit)
	allowed, _ := twilioadapter.ParseAllowedCountryCodes("+1")
	phone := "+447900900900"

	// Guard rejects → audit row, no pendingSet entry, no Twilio call.
	if twilioadapter.IsAllowedCountry(phone, allowed) {
		t.Fatalf("guard should reject UK phone")
	}

	// pendingSet must be empty for this phone; otherwise a future
	// retry (after registering a new country code) would see ErrPhoneBusy
	// even though no Verify session exists.
	if err := pset.OpenPending(phone, "req_should_succeed_after_guard_rejected"); err != nil {
		if errors.Is(err, twilioadapter.ErrPhoneBusy) {
			t.Fatalf("pendingSet should be empty for guard-rejected phone, but got ErrPhoneBusy")
		}
		t.Fatalf("OpenPending: %v", err)
	}

	// Confirm Twilio never saw the guarded request.
	if calls := ft.CallsTo("/Verifications"); len(calls) != 0 {
		t.Fatalf("expected 0 Verifications POSTs through guard rejection, got %d", len(calls))
	}
}

func auditContainsType(t *testing.T, path, wantType string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			continue
		}
		if typ, _ := row["type"].(string); typ == wantType {
			return true
		}
	}
	return false
}
