package twilioadapter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"request-sudo/internal/protocol"
)

// --- stubs ---------------------------------------------------------------

type stubVerify struct {
	mu      sync.Mutex
	calls   int
	result  VerifyCheckResult
	err     error
	lastTo  string
	lastCod string
}

func (s *stubVerify) CheckVerification(_ context.Context, to, code string) (VerifyCheckResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastTo = to
	s.lastCod = code
	return s.result, s.err
}

type brokerCall struct {
	action     string
	requestID  string
	approverID string
	reason     string
}

type stubBroker struct {
	mu    sync.Mutex
	calls []brokerCall
	resp  protocol.Response
	err   error
}

func (s *stubBroker) Call(_ context.Context, action, requestID, _, approverID, reason string) (protocol.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, brokerCall{action: action, requestID: requestID, approverID: approverID, reason: reason})
	return s.resp, s.err
}

type stubPending struct {
	mu        sync.Mutex
	phone     string
	requestID string
	released  []string
}

func (s *stubPending) Resolve(phone string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if phone == s.phone && s.requestID != "" {
		return s.requestID, true
	}
	return "", false
}

func (s *stubPending) Release(phone string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, phone)
	if phone == s.phone {
		s.requestID = ""
	}
}

type stubAudit struct {
	mu   sync.Mutex
	rows []map[string]any
}

func (a *stubAudit) Write(row map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := make(map[string]any, len(row))
	for k, v := range row {
		cp[k] = v
	}
	a.rows = append(a.rows, cp)
	return nil
}

func (a *stubAudit) byType(typ string) []map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []map[string]any
	for _, r := range a.rows {
		if r["type"] == typ {
			out = append(out, r)
		}
	}
	return out
}

// --- helpers -------------------------------------------------------------

func signedRequest(t *testing.T, authToken, publicURL, from, body string) *http.Request {
	t.Helper()
	form := url.Values{}
	form.Set("From", from)
	form.Set("Body", body)
	form.Set("MessageSid", "SM-test")
	sig := computeTwilioSignature(authToken, publicURL, form)
	req := httptest.NewRequest(http.MethodPost, publicURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", sig)
	return req
}

func makeDeps(twilio VerifyChecker, broker BrokerCaller, pending PendingResolver, audit AuditEmitter) InboundReplyDeps {
	return InboundReplyDeps{
		AuthToken:  "test-token",
		PublicURL:  "https://example.com/twilio/inbound",
		ApproverID: "twilio-sms-overnight",
		Twilio:     twilio,
		Broker:     broker,
		Pending:    pending,
		Audit:      audit,
	}
}

// --- ParseReplyIntent ---------------------------------------------------

func TestParseReplyIntent(t *testing.T) {
	cases := []struct {
		body   string
		intent ReplyIntent
		code   string
	}{
		{"yes 123456", ReplyIntentApprove, "123456"},
		{"YES 1234", ReplyIntentApprove, "1234"},
		{"  Yes  98765432  ", ReplyIntentApprove, "98765432"},
		{"no 654321", ReplyIntentDeny, "654321"},
		{"NO 4242", ReplyIntentDeny, "4242"},
		{"123456", ReplyIntentApprove, "123456"},
		{"  4242  ", ReplyIntentApprove, "4242"},
		{"yes 123", ReplyIntentUnknown, ""},      // too short
		{"yes 123456789", ReplyIntentUnknown, ""}, // too long
		{"maybe 123456", ReplyIntentUnknown, ""},
		{"", ReplyIntentUnknown, ""},
		{"junk", ReplyIntentUnknown, ""},
		{"yes 123456\nleak 999999", ReplyIntentUnknown, ""}, // multi-line junk
	}
	for _, tc := range cases {
		gotI, gotC := ParseReplyIntent(tc.body)
		if gotI != tc.intent || gotC != tc.code {
			t.Errorf("ParseReplyIntent(%q) = (%d, %q) want (%d, %q)", tc.body, gotI, gotC, tc.intent, tc.code)
		}
	}
}

// --- HandleInboundReply -------------------------------------------------

func TestHandleInboundReply_YesWithCodeApproves(t *testing.T) {
	twilio := &stubVerify{result: VerifyCheckResult{Approved: true, Status: "approved", SID: "VE-abc"}}
	broker := &stubBroker{resp: protocol.Response{Status: "approved", Message: "ok"}}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-1"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "yes 123456"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 1 {
		t.Fatalf("twilio.CheckVerification calls=%d want 1", twilio.calls)
	}
	if twilio.lastCod != "123456" {
		t.Fatalf("twilio code=%q want 123456", twilio.lastCod)
	}
	if len(broker.calls) != 1 || broker.calls[0].action != protocol.ActionReviewApprove || broker.calls[0].requestID != "REQ-1" {
		t.Fatalf("broker calls=%+v want one review.approve for REQ-1", broker.calls)
	}
	if len(pending.released) != 1 || pending.released[0] != "+13105550100" {
		t.Fatalf("pending.Release=%v want one call for +13105550100", pending.released)
	}
	if got := audit.byType("sms_approval_consumed"); len(got) != 1 {
		t.Fatalf("sms_approval_consumed rows=%d want 1", len(got))
	} else if got[0]["verify_session_sid"] != "VE-abc" {
		t.Fatalf("sms_approval_consumed missing verify_session_sid: %+v", got[0])
	}
}

func TestHandleInboundReply_NoWithCodeDeniesNoTwilio(t *testing.T) {
	twilio := &stubVerify{}
	broker := &stubBroker{resp: protocol.Response{Status: "denied"}}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-2"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "no 123456"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 0 {
		t.Fatalf("twilio.CheckVerification should NOT be called on deny, calls=%d", twilio.calls)
	}
	if len(broker.calls) != 1 || broker.calls[0].action != protocol.ActionReviewDeny || broker.calls[0].requestID != "REQ-2" {
		t.Fatalf("broker calls=%+v want one review.deny for REQ-2", broker.calls)
	}
	if len(pending.released) != 1 {
		t.Fatalf("pending.Release called %d times want 1", len(pending.released))
	}
	if got := audit.byType("sms_denial_consumed"); len(got) != 1 {
		t.Fatalf("sms_denial_consumed rows=%d want 1", len(got))
	}
}

func TestHandleInboundReply_PureDigitsTreatedAsApproval(t *testing.T) {
	twilio := &stubVerify{result: VerifyCheckResult{Approved: true, Status: "approved", SID: "VE-z"}}
	broker := &stubBroker{resp: protocol.Response{Status: "approved"}}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-3"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "424242"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 1 || twilio.lastCod != "424242" {
		t.Fatalf("twilio call=%d code=%q want 1 / 424242", twilio.calls, twilio.lastCod)
	}
	if len(broker.calls) != 1 || broker.calls[0].action != protocol.ActionReviewApprove {
		t.Fatalf("broker calls=%+v want one review.approve", broker.calls)
	}
}

func TestHandleInboundReply_GarbageBodyEmitsUnboundNoBrokerCall(t *testing.T) {
	twilio := &stubVerify{}
	broker := &stubBroker{}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-4"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "lol what"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 0 {
		t.Fatalf("no twilio call expected, got %d", twilio.calls)
	}
	if len(broker.calls) != 0 {
		t.Fatalf("no broker call expected, got %+v", broker.calls)
	}
	if got := audit.byType("verify_unbound_reply"); len(got) != 1 {
		t.Fatalf("verify_unbound_reply rows=%d want 1", len(got))
	}
}

func TestHandleInboundReply_NoPendingForSender(t *testing.T) {
	twilio := &stubVerify{}
	broker := &stubBroker{}
	pending := &stubPending{} // empty: nothing pending
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "yes 123456"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 0 {
		t.Fatalf("no twilio call expected, got %d", twilio.calls)
	}
	if len(broker.calls) != 0 {
		t.Fatalf("no broker call expected, got %+v", broker.calls)
	}
	got := audit.byType("verify_unbound_reply")
	if len(got) != 1 {
		t.Fatalf("verify_unbound_reply rows=%d want 1", len(got))
	}
	if got[0]["reason"] != "no pending request for sender" {
		t.Fatalf("unbound row reason=%v want 'no pending request for sender'", got[0]["reason"])
	}
}

func TestHandleInboundReply_TwilioApprovedFalseEmitsRejectKeepsSlot(t *testing.T) {
	twilio := &stubVerify{result: VerifyCheckResult{Approved: false, Status: "pending", SID: "VE-p"}}
	broker := &stubBroker{}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-5"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "yes 000000"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if twilio.calls != 1 {
		t.Fatalf("expected one twilio check, got %d", twilio.calls)
	}
	if len(broker.calls) != 0 {
		t.Fatalf("no broker call expected on rejection, got %+v", broker.calls)
	}
	if len(pending.released) != 0 {
		t.Fatalf("pending.Release should NOT be called on rejection, got %v", pending.released)
	}
	if got := audit.byType("verify_check_rejected"); len(got) != 1 {
		t.Fatalf("verify_check_rejected rows=%d want 1", len(got))
	}
}

func TestHandleInboundReply_TwilioErrNoPendingEmitsReject(t *testing.T) {
	twilio := &stubVerify{err: ErrNoPendingVerification}
	broker := &stubBroker{}
	pending := &stubPending{phone: "+13105550100", requestID: "REQ-6"}
	audit := &stubAudit{}
	h := HandleInboundReply(makeDeps(twilio, broker, pending, audit))

	w := httptest.NewRecorder()
	h(w, signedRequest(t, "test-token", "https://example.com/twilio/inbound", "+13105550100", "yes 123456"))

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	if len(broker.calls) != 0 {
		t.Fatalf("no broker call expected, got %+v", broker.calls)
	}
	if len(pending.released) != 0 {
		t.Fatalf("pending.Release should NOT be called, got %v", pending.released)
	}
	got := audit.byType("verify_check_rejected")
	if len(got) != 1 {
		t.Fatalf("verify_check_rejected rows=%d want 1", len(got))
	}
	if got[0]["reason"] != "no_pending_verification" {
		t.Fatalf("reject row reason=%v want no_pending_verification", got[0]["reason"])
	}
}

func TestHandleInboundReply_BadSignatureRejected(t *testing.T) {
	deps := makeDeps(&stubVerify{}, &stubBroker{}, &stubPending{}, &stubAudit{})
	h := HandleInboundReply(deps)

	form := url.Values{}
	form.Set("From", "+13105550100")
	form.Set("Body", "yes 123456")
	req := httptest.NewRequest(http.MethodPost, deps.PublicURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", "bogus")

	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

// Sanity: ErrPhoneBusy is wired in twilioadapter; check we can errors.Is
// it (regression for accidental string-equality drift).
func TestErrPhoneBusyIs(t *testing.T) {
	if !errors.Is(ErrPhoneBusy, ErrPhoneBusy) {
		t.Fatal("ErrPhoneBusy must be identity-equal")
	}
}
