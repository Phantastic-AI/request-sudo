// request-sudo-twilio-adapter is the unprivileged out-of-band approval
// channel for request-sudo. It tails the broker's event log, sends SMS
// via Twilio Verify on each new request_created, and listens on a
// localhost webhook port for the operator's reply.
//
// The adapter is ADDITIVE — the broker is fully functional without it
// (dual-mode invariant, RELEASE-BLOCKER per REQUEST_SUDO_V1_GRAPH.md).
//
// Wiring scope (ADR-0005a US-004/US-005):
//   - Send path: country guard → pending-set serialization → Twilio Verify send
//   - Reply path: HMAC → resolve sender → Twilio VerificationCheck → broker review.approve/deny
//   - Twilio owns the verification code; adapter no longer stores code hashes
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"request-sudo/internal/socket"
	"request-sudo/internal/twilioadapter"
)

const defaultEnvPath = "/etc/request-sudo/twilio.env"
const defaultApproversPath = "/etc/request-sudo/approvers.yaml"

func main() {
	var (
		envFile             = flag.String("env-file", defaultEnvPath, "path to twilio.env")
		approversYAML       = flag.String("approvers", defaultApproversPath, "path to approvers.yaml")
		eventLogPath        = flag.String("event-log", "/var/lib/request-sudo/events.jsonl", "broker event log to tail")
		reviewSock          = flag.String("review-socket", "/run/request-sudo/review.sock", "broker review socket")
		auditLogPath        = flag.String("audit-log", "/var/log/request-sudo/twilio-audit.jsonl", "adapter audit log path")
		webhookAddr         = flag.String("webhook-addr", "127.0.0.1:9811", "localhost-only webhook listener address")
		publicURL           = flag.String("public-url", "https://example.com/twilio/inbound", "configured public webhook URL (for HMAC validation)")
		dryRun              = flag.Bool("dry-run", false, "log Twilio calls instead of making them (smoke testing)")
		approverID          = flag.String("approver-id", "twilio-sms-overnight", "actor id used when calling review.approve/deny")
		allowedCountryCodes = flag.String("allowed-country-codes", "+1", "comma-separated E.164 country prefixes allowed for SMS (e.g. '+1,+44')")
		pendingTTL          = flag.Duration("pending-ttl", 10*time.Minute, "per-phone serialization TTL for Verify sessions")
	)
	flag.Parse()

	allowedCodes, err := twilioadapter.ParseAllowedCountryCodes(*allowedCountryCodes)
	if err != nil {
		fatal("parse --allowed-country-codes: %v", err)
	}

	cfg, err := twilioadapter.LoadTwilioEnv(*envFile)
	if err != nil {
		fatal("load twilio env: %v", err)
	}
	policy, err := twilioadapter.LoadApprovers(*approversYAML)
	if err != nil {
		fatal("load approvers.yaml: %v", err)
	}
	audit, err := twilioadapter.NewAuditLog(*auditLogPath)
	if err != nil {
		fatal("audit log: %v", err)
	}
	tailer := twilioadapter.NewCanonTailer(*eventLogPath)
	twilio := twilioadapter.NewTwilioClient(cfg)

	pendingSet := twilioadapter.NewPendingSet(*pendingTTL, audit)
	if err := pendingSet.ReplayFromAudit(audit.Path()); err != nil {
		log.Printf("replay pending-set: %v", err)
	} else {
		log.Printf("replayed pending verifications from audit %s", audit.Path())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Tailer: on each request_created, route + send SMS.
	go func() {
		err := tailer.Run(ctx, func(p twilioadapter.PendingRequest) {
			handlePending(ctx, p, policy, twilio, audit, pendingSet, allowedCodes, *dryRun)
		}, func(err error) {
			log.Printf("canon tail error: %v", err)
		})
		if err != nil {
			log.Printf("canon tail exited: %v", err)
		}
	}()

	// Webhook server: HMAC → parse → resolve → VerificationCheck → broker.
	brokerCaller := twilioadapter.NewBrokerSocketCaller(*reviewSock, socket.Call)
	handler := twilioadapter.HandleInboundReply(twilioadapter.InboundReplyDeps{
		AuthToken:  cfg.AuthToken,
		PublicURL:  *publicURL,
		ApproverID: *approverID,
		Twilio:     twilio,
		Broker:     brokerCaller,
		Pending:    pendingSet,
		Audit:      audit,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/twilio/inbound", handler)
	server := &http.Server{
		Addr:              *webhookAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", *webhookAddr)
	if err != nil {
		fatal("listen %s: %v", *webhookAddr, err)
	}
	go func() {
		log.Printf("request-sudo-twilio-adapter webhook listening on %s", *webhookAddr)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("webhook server: %v", err)
		}
	}()

	log.Printf("request-sudo-twilio-adapter ready (audit=%s, dry-run=%v, allowed_country_codes=%v, pending_ttl=%s)",
		audit.Path(), *dryRun, allowedCodes, *pendingTTL)
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	// Lazy expire on shutdown so audit reflects observed state.
	if n := pendingSet.ExpireDue(); n > 0 {
		log.Printf("expired %d pending verifications on shutdown", n)
	}
}

func handlePending(
	ctx context.Context,
	p twilioadapter.PendingRequest,
	policy twilioadapter.ApproversPolicy,
	twilio *twilioadapter.TwilioClient,
	audit *twilioadapter.AuditLog,
	pending *twilioadapter.PendingSet,
	allowedCountryCodes []string,
	dryRun bool,
) {
	recipients := policy.LookupRecipients(p.Requester)
	if len(recipients) == 0 {
		// T16: empty union → emit sms_routing_empty, fall back to local-only.
		_ = audit.Write(map[string]any{
			"type":       "sms_routing_empty",
			"request_id": p.RequestID,
			"requester":  p.Requester,
		})
		log.Printf("[%s] no phones in routing for requester=%s; falling back to local-only", p.RequestID, p.Requester)
		return
	}

	for _, phone := range recipients {
		maskedPhone := twilioadapter.MaskPhone(phone)

		// US-006: country-code allowlist before any state mutation.
		if !twilioadapter.IsAllowedCountry(phone, allowedCountryCodes) {
			_ = audit.Write(map[string]any{
				"type":             "unsupported_region",
				"request_id":       p.RequestID,
				"recipient_masked": maskedPhone,
			})
			log.Printf("[%s] WARN unsupported region for %s; skipping", p.RequestID, maskedPhone)
			continue
		}

		// US-004: claim per-phone slot BEFORE calling Twilio so a concurrent
		// request_created for the same phone is blocked.
		if err := pending.OpenPending(phone, p.RequestID); err != nil {
			if errors.Is(err, twilioadapter.ErrPhoneBusy) {
				_ = audit.Write(map[string]any{
					"type":             "routing_blocked_same_phone",
					"request_id":       p.RequestID,
					"recipient_masked": maskedPhone,
				})
				log.Printf("[%s] WARN routing blocked: %s has pending Verify within TTL; skipping", p.RequestID, maskedPhone)
				continue
			}
			log.Printf("[%s] OpenPending unexpected error for %s: %v", p.RequestID, maskedPhone, err)
			continue
		}

		if dryRun {
			log.Printf("[%s] DRY-RUN: would Verify-send to %s", p.RequestID, maskedPhone)
			_ = audit.Write(map[string]any{
				"type":               "sms_sent",
				"request_id":         p.RequestID,
				"recipient_masked":   maskedPhone,
				"verify_session_sid": "DRY-RUN",
				"dry_run":            true,
			})
			continue
		}

		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := twilio.SendVerificationSMS(sendCtx, phone, "")
		cancel()
		if err != nil {
			// Per ADR-0005 T25/T27, NEVER auto-retry. Log loudly + audit +
			// rollback the slot we just claimed so the operator can retry.
			log.Printf("[%s] Twilio Verify send FAILED for %s: %v", p.RequestID, maskedPhone, err)
			pending.Release(phone)
			_ = audit.Write(map[string]any{
				"type":             "twilio_send_failed",
				"request_id":       p.RequestID,
				"recipient_masked": maskedPhone,
				"error":            err.Error(),
			})
			continue
		}
		log.Printf("[%s] SMS sent to %s (sid=%s status=%s)", p.RequestID, maskedPhone, result.SID, result.Status)
		_ = audit.Write(map[string]any{
			"type":               "sms_sent",
			"request_id":         p.RequestID,
			"recipient_masked":   maskedPhone,
			"verify_session_sid": result.SID,
			"twilio_status":      result.Status,
		})
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "fatal: "+fmt.Sprintf(format, args...))
	os.Exit(1)
}
