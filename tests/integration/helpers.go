// Package integration holds end-to-end tests that exercise the full
// adapter + broker round-trip. These tests use:
//
//   - An in-process broker.Service (via internal/broker) — same pattern
//     as tests/dualmode/dualmode_test.go.
//   - A FakeTwilio httptest.Server that records Twilio Verify API calls
//     and returns scripted responses, so we never hit real Twilio.
//   - A SandboxPaths helper that gives each test an isolated temp tree
//     with canon, audit log, approvers.yaml, twilio.env.
//
// These helpers are intentionally NOT in internal/twilioadapter so that
// integration tests can import both `request-sudo/internal/twilioadapter`
// and `request-sudo/internal/broker` without circular-import risk.
package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// SandboxPaths captures the directory layout one integration test needs.
type SandboxPaths struct {
	Root          string // temp dir root
	CanonPath     string // broker event log
	AuditPath     string // adapter audit log
	ApproversYaml string // approvers.yaml
	TwilioEnv     string // twilio.env (only present in tests that need it)
}

// NewSandbox creates a fresh temp tree for one integration test.
// Cleans up via t.Cleanup. approverPhones maps approver_id to one or
// more phone numbers; if empty, the approvers.yaml has no `phones`
// entries (tests for the dual-mode no-phones case).
func NewSandbox(t *testing.T, approverPhones map[string][]string) SandboxPaths {
	t.Helper()
	root := t.TempDir()
	sb := SandboxPaths{
		Root:          root,
		CanonPath:     filepath.Join(root, "canon", "events.jsonl"),
		AuditPath:     filepath.Join(root, "audit", "twilio-audit.jsonl"),
		ApproversYaml: filepath.Join(root, "etc", "approvers.yaml"),
		TwilioEnv:     filepath.Join(root, "etc", "twilio.env"),
	}
	for _, d := range []string{
		filepath.Dir(sb.CanonPath),
		filepath.Dir(sb.AuditPath),
		filepath.Dir(sb.ApproversYaml),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(sb.ApproversYaml, buildApproversYaml(approverPhones), 0o640); err != nil {
		t.Fatalf("write approvers.yaml: %v", err)
	}
	return sb
}

// WriteTwilioEnv writes a twilio.env file in the sandbox. The caller
// provides the base URL of a FakeTwilio (or real Twilio) and the four
// secret-shaped strings. baseURL is NOT persisted to the file — the
// adapter still loads the live Twilio URL from its hardcoded const —
// but tests use the FakeTwilio.BaseURL via TwilioClient.WithBaseURL.
func (sb SandboxPaths) WriteTwilioEnv(t *testing.T, accountSID, authToken, verifySID, fromNumber string) {
	t.Helper()
	content := fmt.Sprintf(
		"TWILIO_ACCOUNT_SID=%s\nTWILIO_AUTH_TOKEN=%s\nTWILIO_VERIFY_SERVICE_SID=%s\nTWILIO_FROM_NUMBER=%s\n",
		accountSID, authToken, verifySID, fromNumber,
	)
	if err := os.WriteFile(sb.TwilioEnv, []byte(content), 0o600); err != nil {
		t.Fatalf("write twilio.env: %v", err)
	}
}

func buildApproversYaml(approverPhones map[string][]string) []byte {
	var b strings.Builder
	b.WriteString("approver_sets:\n  operators:\n")
	if len(approverPhones) == 0 {
		b.WriteString("    - testapprover\n")
	} else {
		for k := range approverPhones {
			fmt.Fprintf(&b, "    - %s\n", k)
		}
	}
	b.WriteString("approvers:\n")
	for k, phones := range approverPhones {
		fmt.Fprintf(&b, "  %s:\n", k)
		if len(phones) > 0 {
			b.WriteString("    phones:\n")
			for _, p := range phones {
				fmt.Fprintf(&b, "      - %q\n", p)
			}
		}
	}
	if len(approverPhones) == 0 {
		b.WriteString("  testapprover: {}\n")
	}
	b.WriteString("routing:\n  testuser:\n    approver_set: operators\n    capture_output: plain\n    max_execution_seconds: 60\nwall_notify: false\n")
	return []byte(b.String())
}

// TwilioCall captures one POST to the FakeTwilio server.
type TwilioCall struct {
	Path   string            // request URL path
	Method string            // request method
	Form   map[string]string // form-encoded parameters
	Auth   string            // basic-auth header value (rarely needed for assertions)
}

// FakeTwilio is a recording httptest.Server that mimics the two
// Twilio Verify endpoints the adapter uses:
//
//	POST /v2/Services/{SID}/Verifications      → returns a fake Verification
//	POST /v2/Services/{SID}/VerificationCheck  → returns a fake Check
//
// Default behavior: Verifications returns status="pending" with a
// pseudo-SID; VerificationCheck returns status="approved". Tests can
// override responses via SetVerificationsResponse / SetCheckResponse.
type FakeTwilio struct {
	server   *httptest.Server
	mu       sync.Mutex
	calls    []TwilioCall
	verifyFn http.HandlerFunc
	checkFn  http.HandlerFunc
}

// NewFakeTwilio starts the server. t.Cleanup closes it.
func NewFakeTwilio(t *testing.T) *FakeTwilio {
	t.Helper()
	ft := &FakeTwilio{}
	ft.verifyFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sid":"VEfaketest000000000000000000000001","status":"pending","to":%q,"service_sid":"VAtest","date_created":"2026-05-19T00:00:00Z"}`, r.FormValue("To"))
	}
	ft.checkFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sid":"VEfakecheck00000000000000000000001","status":"approved","to":%q,"date_created":"2026-05-19T00:00:01Z"}`, r.FormValue("To"))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/Services/", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form := map[string]string{}
		for k, vs := range r.PostForm {
			if len(vs) > 0 {
				form[k] = vs[0]
			}
		}
		ft.mu.Lock()
		ft.calls = append(ft.calls, TwilioCall{
			Path:   r.URL.Path,
			Method: r.Method,
			Form:   form,
			Auth:   r.Header.Get("Authorization"),
		})
		verify := ft.verifyFn
		check := ft.checkFn
		ft.mu.Unlock()
		// Route by path suffix; preserves a single-mux model.
		switch {
		case strings.HasSuffix(r.URL.Path, "/Verifications"):
			verify(w, r)
		case strings.HasSuffix(r.URL.Path, "/VerificationCheck"):
			check(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	ft.server = httptest.NewServer(mux)
	t.Cleanup(ft.server.Close)
	return ft
}

// BaseURL returns the URL safe to pass to TwilioClient.WithBaseURL.
func (ft *FakeTwilio) BaseURL() string { return ft.server.URL }

// Calls returns a copy of every recorded request.
func (ft *FakeTwilio) Calls() []TwilioCall {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	out := make([]TwilioCall, len(ft.calls))
	copy(out, ft.calls)
	return out
}

// CallsTo filters Calls() by URL path suffix (e.g. "/Verifications",
// "/VerificationCheck"). Lets tests assert "how many Verifications
// POSTs" without sifting full call list.
func (ft *FakeTwilio) CallsTo(suffix string) []TwilioCall {
	all := ft.Calls()
	out := make([]TwilioCall, 0, len(all))
	for _, c := range all {
		if strings.HasSuffix(c.Path, suffix) {
			out = append(out, c)
		}
	}
	return out
}

// SetVerificationsResponse installs a custom handler for the
// Verifications endpoint. Useful for simulating Twilio failure modes
// (60204 friendly-name reject, 429 rate-limit, 30034 undelivered).
func (ft *FakeTwilio) SetVerificationsResponse(h http.HandlerFunc) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.verifyFn = h
}

// SetCheckResponse installs a custom handler for VerificationCheck.
func (ft *FakeTwilio) SetCheckResponse(h http.HandlerFunc) {
	ft.mu.Lock()
	defer ft.mu.Unlock()
	ft.checkFn = h
}

// VerifyServiceSID matches what the FakeTwilio's default responses use.
// Adapter config must point at this SID for the responses to make sense.
const FakeVerifyServiceSID = "VAtest"

// FormCall is a convenience for asserting against the recorded form.
// Returns map[string]string for a single call; useful in test assertions.
func FormCall(c TwilioCall) map[string]string {
	out := make(map[string]string, len(c.Form))
	for k, v := range c.Form {
		out[k] = v
	}
	return out
}

// AssertFormMissing fails the test if the call's form contains the given key.
// Used for proving "adapter does NOT pass CustomFriendlyName / CustomCode".
func AssertFormMissing(t *testing.T, c TwilioCall, key string) {
	t.Helper()
	if _, ok := c.Form[key]; ok {
		t.Errorf("call to %s should NOT contain form field %q (got %q)", c.Path, key, c.Form[key])
	}
}

// AssertFormEquals fails if the form value for key isn't want.
func AssertFormEquals(t *testing.T, c TwilioCall, key, want string) {
	t.Helper()
	got, ok := c.Form[key]
	if !ok {
		t.Errorf("call to %s missing form field %q (want %q)", c.Path, key, want)
		return
	}
	if got != want {
		t.Errorf("call to %s form[%q] = %q, want %q", c.Path, key, got, want)
	}
}

// Compile-time check that we use net/url somewhere (silence lint when
// helper API evolves and the url import becomes unused temporarily).
var _ = url.Parse
