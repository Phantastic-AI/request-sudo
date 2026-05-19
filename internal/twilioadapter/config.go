// Package twilioadapter implements the request-sudo-twilio-adapter
// behaviors: env loading, approvers.yaml routing, Twilio Verify SMS
// sending, webhook HMAC validation, and reply lifecycle.
//
// Test plan covered in this package (per ADR-0005):
//   - T1–T5: webhook signature validation (webhook.go / webhook_test.go)
//   - T6: 6-digit code generation (code.go / code_test.go)
//   - T13–T16: phone routing default path (routing.go / routing_test.go)
//   - T29–T31: credentials loading (config.go / config_test.go)
//
// Out of scope for the overnight build (deferred to follow-up work):
//   - T7–T12 (full code lifecycle: expiry, single-use, lockout) — only the
//     happy "yes <code>" / "no <code>" parser is wired
//   - T17–T24 (callback hook)
//   - T25–T28 (rate-limit + awaiting_local_approval)
//   - T32–T35 (full canon audit event suite — adapter writes a sandbox
//     audit log instead, keeping the broker as the single canon writer)
package twilioadapter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// TwilioConfig is the credential bundle the adapter loads from twilio.env.
// Maps to ADR-0005 §Credentials.
type TwilioConfig struct {
	AccountSID         string
	AuthToken          string
	VerifyServiceSID   string
	FromNumber         string
	RoutingCallbackURL string
	RoutingCallbackKey string
}

// LoadTwilioEnv reads a key=value env file (subset of POSIX env syntax,
// no exports, no command substitution, comments start with #, blank lines
// ignored). Required keys per ADR-0005 T29.
//
// Returns an error if the file is missing or unreadable (T30).
func LoadTwilioEnv(path string) (TwilioConfig, error) {
	var cfg TwilioConfig
	abs, err := filepath.Abs(path)
	if err != nil {
		return cfg, fmt.Errorf("twilio env path: %w", err)
	}
	file, err := os.Open(abs)
	if err != nil {
		return cfg, fmt.Errorf("open %s: %w", abs, err)
	}
	defer file.Close()

	kv, err := parseEnvFile(file)
	if err != nil {
		return cfg, fmt.Errorf("parse %s: %w", abs, err)
	}

	cfg.AccountSID = kv["TWILIO_ACCOUNT_SID"]
	cfg.AuthToken = kv["TWILIO_AUTH_TOKEN"]
	cfg.VerifyServiceSID = kv["TWILIO_VERIFY_SERVICE_SID"]
	cfg.FromNumber = kv["TWILIO_FROM_NUMBER"]
	cfg.RoutingCallbackURL = kv["SMS_ROUTING_CALLBACK_URL"]
	cfg.RoutingCallbackKey = kv["SMS_ROUTING_CALLBACK_SECRET"]

	if cfg.AccountSID == "" {
		return cfg, errors.New("TWILIO_ACCOUNT_SID missing")
	}
	if cfg.AuthToken == "" {
		return cfg, errors.New("TWILIO_AUTH_TOKEN missing")
	}
	if cfg.VerifyServiceSID == "" {
		return cfg, errors.New("TWILIO_VERIFY_SERVICE_SID missing")
	}
	// TWILIO_FROM_NUMBER is intentionally NOT required: ADR-0005a moved the
	// send-path to Twilio Verify, which routes via Twilio's managed sender
	// pool. The field is still read so legacy twilio.env files keep loading,
	// but the Verify path never consumes it. Honors the 3-env-var
	// portability promise in docs/twilio-setup.md.
	if cfg.RoutingCallbackURL != "" && cfg.RoutingCallbackKey == "" {
		return cfg, errors.New("SMS_ROUTING_CALLBACK_URL set without SMS_ROUTING_CALLBACK_SECRET")
	}
	return cfg, nil
}

func parseEnvFile(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// strip leading "export " for compatibility with shell-style files
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("invalid env line: %q", line)
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
