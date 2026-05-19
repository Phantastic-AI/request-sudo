package twilioadapter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"request-sudo/internal/core"
	"request-sudo/internal/protocol"
)

// VerifyChecker is the Twilio surface the webhook handler needs. The
// production implementation is *TwilioClient; tests use a stub.
type VerifyChecker interface {
	CheckVerification(ctx context.Context, to, code string) (VerifyCheckResult, error)
}

// BrokerCaller is the broker review-socket surface the webhook handler
// needs. The production implementation calls socket.Call against the
// review socket path; tests use a stub.
type BrokerCaller interface {
	Call(ctx context.Context, action, requestID, approverKind, approverID, reason string) (protocol.Response, error)
}

// PendingResolver is the pending-set surface the webhook handler needs.
// *PendingSet satisfies this; tests use a stub.
type PendingResolver interface {
	Resolve(phone string) (string, bool)
	Release(phone string)
}

// AuditEmitter is the audit-sink surface the webhook handler needs.
// *AuditLog satisfies this via Write; tests use a stub.
type AuditEmitter interface {
	Write(row map[string]any) error
}

// ReplyIntent is the parsed shape of an inbound reply body under
// ADR-0005a US-005. Body forms (case-insensitive, leading/trailing
// whitespace tolerated, no globbing into multi-line):
//
//	"yes <4-8 digits>"  → ReplyIntentApprove
//	"no  <4-8 digits>"  → ReplyIntentDeny
//	"<4-8 digits>"      → ReplyIntentApprove (operator may text code only)
//	anything else       → ReplyIntentUnknown
type ReplyIntent int

const (
	ReplyIntentUnknown ReplyIntent = iota
	ReplyIntentApprove
	ReplyIntentDeny
)

// Regex notes:
//   - Anchored with \A and \z so multi-line bodies cannot smuggle a
//     trailing approval token after junk.
//   - (?i) for case-insensitive "yes"/"no".
//   - Code is 4-8 digits per US-005 brief.
var (
	reYesCode = regexp.MustCompile(`\A\s*(?i:yes)\s+(\d{4,8})\s*\z`)
	reNoCode  = regexp.MustCompile(`\A\s*(?i:no)\s+(\d{4,8})\s*\z`)
	reDigits  = regexp.MustCompile(`\A\s*(\d{4,8})\s*\z`)
)

// ParseReplyIntent parses an inbound SMS body into an intent + extracted
// code. See ReplyIntent for accepted forms.
func ParseReplyIntent(body string) (ReplyIntent, string) {
	if m := reYesCode.FindStringSubmatch(body); m != nil {
		return ReplyIntentApprove, m[1]
	}
	if m := reNoCode.FindStringSubmatch(body); m != nil {
		return ReplyIntentDeny, m[1]
	}
	if m := reDigits.FindStringSubmatch(body); m != nil {
		return ReplyIntentApprove, m[1]
	}
	return ReplyIntentUnknown, ""
}

// InboundReplyDeps bundles the collaborators the webhook handler needs.
// Keeping this as a struct keeps the route wiring in main.go compact
// while letting tests inject stubs.
type InboundReplyDeps struct {
	AuthToken    string
	PublicURL    string
	ApproverID   string
	Twilio       VerifyChecker
	Broker       BrokerCaller
	Pending      PendingResolver
	Audit        AuditEmitter
	BrokerTimeout time.Duration
}

// HandleInboundReply is the HTTP handler for POST /twilio/inbound under
// ADR-0005a US-005. It is exported so tests can drive it without
// standing up the full main.go server.
//
// Flow:
//  1. Validate Twilio HMAC (existing webhook.go). 401 on bad/missing.
//  2. Parse reply via ParseReplyIntent.
//  3. Resolve sender phone → pending request_id; emit verify_unbound_reply on miss.
//  4. Approve: VerificationCheck; on Approved=true call broker.review.approve, then Release.
//     On rejection: emit verify_check_rejected; do NOT release (operator can retry within TTL).
//  5. Deny: skip Twilio; call broker.review.deny; on success Release.
func HandleInboundReply(deps InboundReplyDeps) http.HandlerFunc {
	timeout := deps.BrokerTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("X-Twilio-Signature")
		if !ValidateTwilioSignature(deps.AuthToken, deps.PublicURL, r.PostForm, sig) {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		from := r.PostForm.Get("From")
		body := r.PostForm.Get("Body")

		intent, code := ParseReplyIntent(body)
		if intent == ReplyIntentUnknown {
			emitUnbound(deps.Audit, "", from, "unparseable body", body)
			emptyTwiML(w)
			return
		}

		requestID, ok := deps.Pending.Resolve(from)
		if !ok {
			emitUnbound(deps.Audit, "", from, "no pending request for sender", body)
			emptyTwiML(w)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		switch intent {
		case ReplyIntentApprove:
			result, err := deps.Twilio.CheckVerification(ctx, from, code)
			if err != nil || !result.Approved {
				rejectInfo := map[string]any{
					"type":             "verify_check_rejected",
					"request_id":       requestID,
					"recipient_masked": MaskPhone(from),
					"verify_status":    result.Status,
				}
				if err != nil {
					if errors.Is(err, ErrNoPendingVerification) {
						rejectInfo["reason"] = "no_pending_verification"
					} else {
						rejectInfo["error"] = err.Error()
					}
				}
				if result.SID != "" {
					rejectInfo["verify_session_sid"] = result.SID
				}
				_ = deps.Audit.Write(rejectInfo)
				log.Printf("[%s] verify check rejected for %s (status=%s err=%v)", requestID, MaskPhone(from), result.Status, err)
				emptyTwiML(w)
				return
			}
			resp, err := deps.Broker.Call(ctx, protocol.ActionReviewApprove, requestID, "twilio", deps.ApproverID, "approved via SMS")
			if err != nil {
				log.Printf("[%s] broker review.approve failed: %v", requestID, err)
				emptyTwiML(w)
				return
			}
			deps.Pending.Release(from)
			_ = deps.Audit.Write(map[string]any{
				"type":               "sms_approval_consumed",
				"request_id":         requestID,
				"recipient_masked":   MaskPhone(from),
				"verify_session_sid": result.SID,
				"broker_status":      resp.Status,
				"broker_message":     resp.Message,
			})
			emptyTwiML(w)
			return

		case ReplyIntentDeny:
			resp, err := deps.Broker.Call(ctx, protocol.ActionReviewDeny, requestID, "twilio", deps.ApproverID, "sms_denial")
			if err != nil {
				log.Printf("[%s] broker review.deny failed: %v", requestID, err)
				emptyTwiML(w)
				return
			}
			deps.Pending.Release(from)
			_ = deps.Audit.Write(map[string]any{
				"type":             "sms_denial_consumed",
				"request_id":       requestID,
				"recipient_masked": MaskPhone(from),
				"broker_status":    resp.Status,
				"broker_message":   resp.Message,
			})
			emptyTwiML(w)
			return
		}
	}
}

func emitUnbound(audit AuditEmitter, requestID, from, reason, body string) {
	preview := body
	if len(preview) > 40 {
		preview = preview[:40]
	}
	row := map[string]any{
		"type":             "verify_unbound_reply",
		"recipient_masked": MaskPhone(from),
		"reason":           reason,
		"body_preview":     preview,
	}
	if requestID != "" {
		row["request_id"] = requestID
	}
	_ = audit.Write(row)
}

func emptyTwiML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
}

// brokerSocketCaller adapts the existing socket.Call into a BrokerCaller
// without dragging the socket package into the handler tests. main.go
// constructs it once at startup. Keeping it here (not main.go) so its
// shape lives next to the interface it satisfies.
type brokerSocketCaller struct {
	socketPath string
	dialer     func(ctx context.Context, socketPath string, req protocol.Request) (protocol.Response, error)
}

// NewBrokerSocketCaller wires a BrokerCaller to the broker's review
// socket. The dialer parameter is the existing socket.Call function;
// passing it explicitly avoids a circular import.
func NewBrokerSocketCaller(socketPath string, dialer func(ctx context.Context, socketPath string, req protocol.Request) (protocol.Response, error)) BrokerCaller {
	return &brokerSocketCaller{socketPath: socketPath, dialer: dialer}
}

func (b *brokerSocketCaller) Call(ctx context.Context, action, requestID, approverKind, approverID, reason string) (protocol.Response, error) {
	req := protocol.Request{
		Action:    action,
		RequestID: requestID,
		Approver:  core.Actor{Kind: approverKind, ID: approverID},
		Reason:    reason,
	}
	return b.dialer(ctx, b.socketPath, req)
}

