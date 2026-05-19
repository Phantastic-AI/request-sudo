// Package dualmode verifies the v1 release-blocker invariant from
// REQUEST_SUDO_V1_GRAPH.md §"Dual-mode invariant":
//
//   1. No request-sudo-twilio-adapter binary installed  → broker works.
//   2. No /etc/request-sudo/twilio.env file              → broker works.
//   3. No `phones:` entries in approvers.yaml            → routing falls
//      back to local-only, broker works.
//
// These tests do NOT require Twilio. They only test the broker's
// behavior when the Twilio adapter is absent or starved, and the
// adapter's graceful degradation when its config is incomplete.
package dualmode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"request-sudo/internal/broker"
	"request-sudo/internal/core"
	"request-sudo/internal/events"
	"request-sudo/internal/execution"
	"request-sudo/internal/protocol"
	"request-sudo/internal/twilioadapter"
)

// Invariant #1: broker stands up and accepts a submission with no
// adapter present. We exercise the broker's Submit/Approve/Execute
// path directly — no adapter binary involved.
func TestInvariant1_BrokerWorksWithoutAdapter(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	log, err := events.NewLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := broker.NewService(log, execution.LocalExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := svc.Submit(ctx, broker.SubmitInput{
		Argv:      []string{"/bin/echo", "hello"},
		Requester: core.PeerIdentity{UID: 1000, Username: "testuser"},
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if resp.Status != string(core.StatusPending) {
		t.Fatalf("status=%s want pending", resp.Status)
	}
	// Local approve works, adapter or not.
	resp, err = svc.Approve(ctx, broker.ReviewInput{
		RequestID: resp.RequestID,
		Approver:  core.Actor{Kind: "local", ID: "operator"},
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if resp.Status != string(core.StatusApproved) {
		t.Fatalf("status=%s want approved", resp.Status)
	}
	resp, err = svc.Execute(ctx, resp.RequestID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Status != string(core.StatusExecuted) {
		t.Fatalf("status=%s want executed; msg=%s", resp.Status, resp.Message)
	}
	if resp.Stdout != "hello\n" {
		t.Fatalf("stdout=%q want %q", resp.Stdout, "hello\n")
	}
	_ = protocol.ActionRequestSubmit // sanity import
}

// Invariant #2: no twilio.env file → adapter refuses to start, but
// broker still works (covered by #1). Here we verify the adapter's
// LoadTwilioEnv loud-fails, which is the documented "hard startup
// failure" per ADR-0005 T30. The broker is unaffected — we don't even
// need to spin one up.
func TestInvariant2_AdapterRefusesWithoutEnvFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "twilio.env.does.not.exist")
	_, err := twilioadapter.LoadTwilioEnv(missing)
	if err == nil {
		t.Fatal("expected hard failure for missing twilio.env")
	}
	// Verify the error mentions the path so an operator can debug it.
	if !contains(err.Error(), "open") {
		t.Fatalf("error should mention 'open': %v", err)
	}
}

// Invariant #3: approvers.yaml with no `phones:` entries → adapter
// routing falls back to local-only (emits sms_routing_empty, never
// calls Twilio). Broker is independent — we again verify by running
// Submit/Approve directly.
func TestInvariant3_NoPhonesFallsBackToLocalOnly(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "approvers.yaml")
	body := `approver_sets:
  operators:
    - phoneless
approvers:
  phoneless:
    phones:
routing:
  testuser:
    approver_set: operators
`
	if err := os.WriteFile(yamlPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := twilioadapter.LoadApprovers(yamlPath)
	if err != nil {
		t.Fatalf("LoadApprovers: %v", err)
	}
	recipients := policy.LookupRecipients("testuser")
	if len(recipients) != 0 {
		t.Fatalf("expected empty recipients (local-only fallback), got %v", recipients)
	}

	// And the broker still approves locally without ever talking to an
	// adapter. Same as invariant #1 but emphasizing the contract.
	logPath := filepath.Join(dir, "events.jsonl")
	log, err := events.NewLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := broker.NewService(log, execution.LocalExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := svc.Submit(ctx, broker.SubmitInput{
		Argv:      []string{"/bin/echo", "local-fallback"},
		Requester: core.PeerIdentity{UID: 1000, Username: "testuser"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != string(core.StatusPending) {
		t.Fatalf("status=%s", resp.Status)
	}
	resp, err = svc.Approve(ctx, broker.ReviewInput{
		RequestID: resp.RequestID,
		Approver:  core.Actor{Kind: "local", ID: "anybody-but-testuser"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != string(core.StatusApproved) {
		t.Fatalf("status=%s want approved", resp.Status)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
