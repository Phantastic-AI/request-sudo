package twilioadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T29–T31: credentials loading.

func TestLoadTwilioEnv_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio.env")
	body := `# comment
TWILIO_ACCOUNT_SID=AC123
TWILIO_AUTH_TOKEN=secret
TWILIO_VERIFY_SERVICE_SID=VA456
TWILIO_FROM_NUMBER=+15551001
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTwilioEnv(path)
	if err != nil {
		t.Fatalf("LoadTwilioEnv: %v", err)
	}
	if cfg.AccountSID != "AC123" || cfg.AuthToken != "secret" || cfg.VerifyServiceSID != "VA456" || cfg.FromNumber != "+15551001" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadTwilioEnv_MissingFile_T30(t *testing.T) {
	_, err := LoadTwilioEnv("/nonexistent/twilio.env")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Fatalf("expected open error, got: %v", err)
	}
}

func TestLoadTwilioEnv_MissingRequiredKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio.env")
	body := "TWILIO_ACCOUNT_SID=AC123\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTwilioEnv(path)
	if err == nil {
		t.Fatal("expected error for missing keys")
	}
	if !strings.Contains(err.Error(), "TWILIO_AUTH_TOKEN") {
		t.Fatalf("expected complaint about TWILIO_AUTH_TOKEN, got: %v", err)
	}
}

func TestLoadTwilioEnv_OptionalCallbackKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio.env")
	body := `TWILIO_ACCOUNT_SID=AC123
TWILIO_AUTH_TOKEN=secret
TWILIO_VERIFY_SERVICE_SID=VA456
TWILIO_FROM_NUMBER=+15551001
SMS_ROUTING_CALLBACK_URL=http://localhost:9810/route
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTwilioEnv(path)
	if err == nil {
		t.Fatal("expected error: callback URL without secret")
	}
}

// ADR-0005a T35: OSS portability — adapter MUST load with just three
// env vars (Account SID + Auth Token + Verify Service SID). FromNumber
// is legacy/optional under the Verify-as-transport model.
func TestLoadTwilioEnv_ThreeVarPortability(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio.env")
	body := `TWILIO_ACCOUNT_SID=AC123
TWILIO_AUTH_TOKEN=secret
TWILIO_VERIFY_SERVICE_SID=VA456
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTwilioEnv(path)
	if err != nil {
		t.Fatalf("LoadTwilioEnv with 3 env vars should succeed: %v", err)
	}
	if cfg.AccountSID != "AC123" || cfg.AuthToken != "secret" || cfg.VerifyServiceSID != "VA456" {
		t.Fatalf("required fields not parsed: %+v", cfg)
	}
	if cfg.FromNumber != "" {
		t.Fatalf("FromNumber should be empty when omitted, got %q", cfg.FromNumber)
	}
}

func TestLoadTwilioEnv_QuotedValuesAndComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "twilio.env")
	body := `# header comment
TWILIO_ACCOUNT_SID="AC123"
TWILIO_AUTH_TOKEN='secret with spaces'
TWILIO_VERIFY_SERVICE_SID=VA456
TWILIO_FROM_NUMBER=+15551001
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadTwilioEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AccountSID != "AC123" || cfg.AuthToken != "secret with spaces" {
		t.Fatalf("unexpected: %+v", cfg)
	}
}
