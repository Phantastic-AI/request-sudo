package twilioadapter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// T13–T16: phone routing default path.

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "approvers.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApprovers_T13_SinglePhonePerApprover(t *testing.T) {
	body := `approver_sets:
  operators:
    - aditya
approvers:
  aditya:
    phones:
      - "+15551234"
routing:
  testuser:
    approver_set: operators
`
	path := writeYAML(t, body)
	policy, err := LoadApprovers(path)
	if err != nil {
		t.Fatalf("LoadApprovers: %v", err)
	}
	got := policy.LookupRecipients("testuser")
	want := []string{"+15551234"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestApprovers_T14_UnionAcrossSet(t *testing.T) {
	body := `approver_sets:
  operators:
    - aditya
    - dominic
approvers:
  aditya:
    phones:
      - "+15551001"
  dominic:
    phones:
      - "+15551002"
routing:
  testuser:
    approver_set: operators
`
	path := writeYAML(t, body)
	policy, err := LoadApprovers(path)
	if err != nil {
		t.Fatalf("LoadApprovers: %v", err)
	}
	got := policy.LookupRecipients("testuser")
	want := []string{"+15551001", "+15551002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestApprovers_T15_SkipApproverWithNoPhones(t *testing.T) {
	body := `approver_sets:
  operators:
    - aditya
    - phoneless
approvers:
  aditya:
    phones:
      - "+15551001"
  phoneless:
    phones:
routing:
  testuser:
    approver_set: operators
`
	path := writeYAML(t, body)
	policy, err := LoadApprovers(path)
	if err != nil {
		t.Fatalf("LoadApprovers: %v", err)
	}
	got := policy.LookupRecipients("testuser")
	want := []string{"+15551001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestApprovers_T16_EmptyUnionFallback(t *testing.T) {
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
	path := writeYAML(t, body)
	policy, err := LoadApprovers(path)
	if err != nil {
		t.Fatalf("LoadApprovers: %v", err)
	}
	got := policy.LookupRecipients("testuser")
	if len(got) != 0 {
		t.Fatalf("expected empty fallback, got %v", got)
	}
}

func TestApprovers_DeduplicatesPhones(t *testing.T) {
	body := `approver_sets:
  operators:
    - aditya
    - aditya2
approvers:
  aditya:
    phones:
      - "+15551234"
  aditya2:
    phones:
      - "+15551234"
routing:
  testuser:
    approver_set: operators
`
	path := writeYAML(t, body)
	policy, err := LoadApprovers(path)
	if err != nil {
		t.Fatal(err)
	}
	got := policy.LookupRecipients("testuser")
	if len(got) != 1 || got[0] != "+15551234" {
		t.Fatalf("dedup failed: %v", got)
	}
}

func TestApprovers_OvernightSandboxFile(t *testing.T) {
	// Smoke-test the actual sandbox approvers.yaml so a stray YAML
	// indentation bug surfaces before we wire it to a real Twilio call.
	const sandbox = "/srv/moltpod-src/security/request-sudo/.test-overnight/etc/approvers.yaml"
	if _, err := os.Stat(sandbox); err != nil {
		t.Skipf("sandbox approvers.yaml not present: %v", err)
	}
	policy, err := LoadApprovers(sandbox)
	if err != nil {
		t.Fatalf("LoadApprovers(sandbox): %v", err)
	}
	got := policy.LookupRecipients("aditya")
	if len(got) != 1 || got[0] != "+13102957704" {
		t.Fatalf("expected aditya routing to +13102957704, got %v", got)
	}
}
