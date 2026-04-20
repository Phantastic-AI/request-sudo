package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type submitRequest struct {
	Action string   `json:"action"`
	Argv   []string `json:"argv"`
	Reason string   `json:"reason"`
	Mode   string   `json:"mode"`
}

type submitResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

type statusResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type executeRequest struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
}

type executeResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
}

type eventSequence struct {
	Events []string `json:"events"`
}

type transitionSet struct {
	Allowed   [][]string `json:"allowed"`
	Forbidden [][]string `json:"forbidden"`
}

type smokePath struct {
	Name       string      `json:"name"`
	Sockets    socketPaths `json:"sockets"`
	Steps      []smokeStep `json:"steps"`
	NonGoals   []string    `json:"non_goals"`
	Invariants []string    `json:"invariants"`
}

type socketPaths struct {
	Request string `json:"request"`
	Review  string `json:"review"`
}

type smokeStep struct {
	Name           string `json:"name"`
	Actor          string `json:"actor"`
	Socket         string `json:"socket"`
	Action         string `json:"action,omitempty"`
	ExpectedStatus string `json:"expected_status,omitempty"`
	EmitsEvent     string `json:"emits_event,omitempty"`
}

var requestIDPattern = regexp.MustCompile(`^req_[a-z0-9]+$`)

func TestSubmitFixturesMatchRequesterContract(t *testing.T) {
	req := readJSON[submitRequest](t, "testdata/protocol/request_submit.json")
	if req.Action != "request.submit" {
		t.Fatalf("unexpected action: %q", req.Action)
	}
	if len(req.Argv) == 0 {
		t.Fatal("argv must not be empty")
	}
	if req.Mode != "poll" && req.Mode != "wait" {
		t.Fatalf("unexpected mode: %q", req.Mode)
	}

	resp := readJSON[submitResponse](t, "testdata/protocol/request_submit_response_pending.json")
	if !requestIDPattern.MatchString(resp.RequestID) {
		t.Fatalf("request_id must match %s, got %q", requestIDPattern.String(), resp.RequestID)
	}
	if resp.Status != "pending" {
		t.Fatalf("expected pending status, got %q", resp.Status)
	}
	if resp.Message == "" {
		t.Fatal("pending response should include a human-readable message")
	}
}

func TestStatusAndExecuteFixturesMatchRequesterContract(t *testing.T) {
	approved := readJSON[statusResponse](t, "testdata/protocol/request_status_approved.json")
	if approved.Status != "approved" {
		t.Fatalf("expected approved status, got %q", approved.Status)
	}
	if !requestIDPattern.MatchString(approved.RequestID) {
		t.Fatalf("approved response request_id invalid: %q", approved.RequestID)
	}

	denied := readJSON[statusResponse](t, "testdata/protocol/request_status_denied.json")
	if denied.Status != "denied" {
		t.Fatalf("expected denied status, got %q", denied.Status)
	}
	if denied.Message == "" {
		t.Fatal("denied response should include a human-readable message")
	}

	execute := readJSON[executeRequest](t, "testdata/protocol/request_execute.json")
	if execute.Action != "request.execute" {
		t.Fatalf("unexpected execute action: %q", execute.Action)
	}
	if !requestIDPattern.MatchString(execute.RequestID) {
		t.Fatalf("execute request request_id invalid: %q", execute.RequestID)
	}

	executed := readJSON[executeResponse](t, "testdata/protocol/request_execute_response_executed.json")
	if executed.Status != "executed" {
		t.Fatalf("expected executed status, got %q", executed.Status)
	}
	if executed.ExitCode != 0 {
		t.Fatalf("expected exit_code 0, got %d", executed.ExitCode)
	}

	rejected := readJSON[executeResponse](t, "testdata/protocol/request_execute_response_rejected.json")
	if rejected.Status != "rejected" {
		t.Fatalf("expected rejected status, got %q", rejected.Status)
	}
	if rejected.Message == "" {
		t.Fatal("rejected execute response should include a reason")
	}
}

func TestStateMachineFixturesProtectSingleExecution(t *testing.T) {
	transitions := readJSON[transitionSet](t, "testdata/state_machine/transitions.json")
	if len(transitions.Allowed) == 0 || len(transitions.Forbidden) == 0 {
		t.Fatal("expected both allowed and forbidden transitions")
	}

	allowed := make(map[[2]string]bool)
	for _, pair := range transitions.Allowed {
		if len(pair) != 2 {
			t.Fatalf("allowed transition must contain exactly two states: %#v", pair)
		}
		allowed[[2]string{pair[0], pair[1]}] = true
	}

	requiredAllowed := [][2]string{{"pending", "approved"}, {"approved", "executing"}, {"executing", "executed"}, {"executing", "failed"}}
	for _, pair := range requiredAllowed {
		if !allowed[pair] {
			t.Fatalf("missing required allowed transition %q -> %q", pair[0], pair[1])
		}
	}

	for _, pair := range transitions.Forbidden {
		if len(pair) != 2 {
			t.Fatalf("forbidden transition must contain exactly two states: %#v", pair)
		}
		key := [2]string{pair[0], pair[1]}
		if allowed[key] {
			t.Fatalf("transition %q -> %q cannot be both allowed and forbidden", pair[0], pair[1])
		}
	}
}

func TestMinimumAuditSequencesMatchStateMachineDoc(t *testing.T) {
	assertSequence(t, "testdata/events/success_sequence.json", []string{"request_created", "request_approved", "execution_started", "execution_succeeded"})
	assertSequence(t, "testdata/events/failure_sequence.json", []string{"request_created", "request_approved", "execution_started", "execution_failed"})
	assertSequence(t, "testdata/events/recovery_sequence.json", []string{"request_created", "request_approved", "execution_started", "recovery_marked_failed"})
}

func TestProtocolAndStateMachineDocsCoverCoreContractRules(t *testing.T) {
	protocolData, err := os.ReadFile(filepath.Clean("../../PROTOCOL.md"))
	if err != nil {
		t.Fatalf("read PROTOCOL.md: %v", err)
	}
	protocolContent := string(protocolData)

	protocolSnippets := []string{
		"/run/request-sudo/request.sock",
		"/run/request-sudo/review.sock",
		"The requester socket and review socket must never be treated as equivalent.",
		"request.execute",
		"must never launch a second execve for the same request",
	}

	for _, snippet := range protocolSnippets {
		if !strings.Contains(protocolContent, snippet) {
			t.Fatalf("PROTOCOL.md missing required snippet %q", snippet)
		}
	}

	stateData, err := os.ReadFile(filepath.Clean("../../STATE_MACHINE.md"))
	if err != nil {
		t.Fatalf("read STATE_MACHINE.md: %v", err)
	}
	stateContent := string(stateData)

	stateSnippets := []string{
		"denied -> approved",
		"executed -> executing",
		"recovery_marked_failed",
		"do **not** re-run automatically",
		"never silently replay execution",
	}

	for _, snippet := range stateSnippets {
		if !strings.Contains(stateContent, snippet) {
			t.Fatalf("STATE_MACHINE.md missing required snippet %q", snippet)
		}
	}
}

func TestPhase1ReviewAndVerificationDocsCoverGuardrails(t *testing.T) {
	reviewData, err := os.ReadFile(filepath.Clean("../../docs/PHASE1_REVIEW.md"))
	if err != nil {
		t.Fatalf("read docs/PHASE1_REVIEW.md: %v", err)
	}
	reviewContent := string(reviewData)

	reviewSnippets := []string{
		"`request-sudod` is the only writer for durable state",
		"/run/request-sudo/request.sock",
		"/run/request-sudo/review.sock",
		"Do not collapse both paths into one listener",
		"No plugin-first path",
	}

	for _, snippet := range reviewSnippets {
		if !strings.Contains(reviewContent, snippet) {
			t.Fatalf("docs/PHASE1_REVIEW.md missing required snippet %q", snippet)
		}
	}

	verificationData, err := os.ReadFile(filepath.Clean("../../docs/VERIFICATION.md"))
	if err != nil {
		t.Fatalf("read docs/VERIFICATION.md: %v", err)
	}
	verificationContent := string(verificationData)

	verificationSnippets := []string{
		"./scripts/verify-contracts.sh",
		"go test ./...",
		"go test -race ./...",
		"go vet ./...",
		"recovery from dangling `executing` writes a failure-style repair event instead of replaying work",
	}

	for _, snippet := range verificationSnippets {
		if !strings.Contains(verificationContent, snippet) {
			t.Fatalf("docs/VERIFICATION.md missing required snippet %q", snippet)
		}
	}
}

func TestSmokePathDocumentCoversRecoveryAndNonGoals(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean("../../docs/SMOKE_PATH.md"))
	if err != nil {
		t.Fatalf("read docs/SMOKE_PATH.md: %v", err)
	}
	content := string(data)

	requiredSnippets := []string{
		"recovery_marked_failed",
		"remote approval transport",
		"plugin-mediated request capture",
		"second `request-sudo execute req_<opaque>` does not run another command",
	}

	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("docs/SMOKE_PATH.md missing required snippet %q", snippet)
		}
	}
}

func TestSmokePathFixtureMatchesFrozenDesign(t *testing.T) {
	smoke := readJSON[smokePath](t, "testdata/smoke/local_manual_approval.json")
	if smoke.Sockets.Request != "/run/request-sudo/request.sock" {
		t.Fatalf("unexpected requester socket: %q", smoke.Sockets.Request)
	}
	if smoke.Sockets.Review != "/run/request-sudo/review.sock" {
		t.Fatalf("unexpected review socket: %q", smoke.Sockets.Review)
	}
	if len(smoke.Steps) != 4 {
		t.Fatalf("expected 4 smoke-path steps, got %d", len(smoke.Steps))
	}

	expected := []struct {
		Name, Actor, Socket string
	}{
		{"submit_request", "requester", "/run/request-sudo/request.sock"},
		{"review_request", "approver", "/run/request-sudo/review.sock"},
		{"execute_request", "requester", "/run/request-sudo/request.sock"},
		{"verify_audit_and_recovery", "operator", "/run/request-sudo/review.sock"},
	}

	for i, step := range smoke.Steps {
		if step.Name != expected[i].Name {
			t.Fatalf("step %d name mismatch: got %q want %q", i, step.Name, expected[i].Name)
		}
		if step.Actor != expected[i].Actor {
			t.Fatalf("step %d actor mismatch: got %q want %q", i, step.Actor, expected[i].Actor)
		}
		if step.Socket != expected[i].Socket {
			t.Fatalf("step %d socket mismatch: got %q want %q", i, step.Socket, expected[i].Socket)
		}
	}

	assertContains(t, smoke.Invariants, "single-writer broker owns all state transitions")
	assertContains(t, smoke.Invariants, "exact argv executes verbatim; summaries may be rewritten")
	assertContains(t, smoke.NonGoals, "plugin-first execution paths")
}

func assertSequence(t *testing.T, path string, want []string) {
	t.Helper()
	sequence := readJSON[eventSequence](t, path)
	if len(sequence.Events) != len(want) {
		t.Fatalf("unexpected sequence length for %s: got %d want %d", path, len(sequence.Events), len(want))
	}
	for i, got := range sequence.Events {
		if got != want[i] {
			t.Fatalf("unexpected event at %s[%d]: got %q want %q", path, i, got, want[i])
		}
	}
}

func assertContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("missing expected value %q in %#v", want, values)
}

func readJSON[T any](t *testing.T, relativePath string) T {
	t.Helper()
	path := filepath.Clean(relativePath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return out
}

func TestInstallerAndSmokeArtifactsExist(t *testing.T) {
	for _, path := range []string{
		filepath.Clean("../../scripts/install.sh"),
		filepath.Clean("../../scripts/smoke-local.sh"),
		filepath.Clean("../../packaging/systemd/request-sudod.service.tmpl"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing required artifact %s: %v", path, err)
		}
	}
}
