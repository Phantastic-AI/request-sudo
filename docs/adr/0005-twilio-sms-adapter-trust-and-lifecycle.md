---
status: accepted
date: 2026-05-11
---

# Twilio SMS adapter: trust model, approval-code lifecycle, and routing hook

The v1 out-of-band approval channel is **Twilio SMS**. A small unprivileged adapter process (`request-sudo-twilio-adapter`) sits between the broker and Twilio: it sends the approval-prompt SMS, receives the operator's reply via a Twilio webhook, validates the reply, and calls `review.approve` / `review.deny` on the broker's review socket. This ADR locks the trust model (how the adapter authenticates itself to Twilio and to the broker, and how the broker trusts the adapter), the approval-code lifecycle, the optional routing hook for deployment-specific SMS recipients (e.g. moltpod's admin-impersonation override), and the rate-limit-handling story (informed by Twilio incidents on the existing airlock-svc).

## Test plan (must all be red before any implementation)

### Twilio webhook signature validation

- **T1.** Webhook POST to the adapter with a valid `X-Twilio-Signature` header (HMAC-SHA1 of the full URL plus sorted form parameters, computed with the configured Twilio auth token) is accepted.
- **T2.** Webhook POST with a missing `X-Twilio-Signature` header is rejected with HTTP 401.
- **T3.** Webhook POST with a wrong signature (e.g. computed with a different auth token, or with the body tampered) is rejected with HTTP 401.
- **T4.** Webhook POST whose `Host` header does not match the adapter's configured public URL is rejected with HTTP 401 (the signed URL prefix must match).
- **T5.** Signature comparison uses constant-time equality (no early-exit timing leak).

### Approval-code lifecycle

- **T6.** Approval code is exactly 6 numeric digits (`000000`–`999999`), generated from a cryptographically secure RNG.
- **T7.** Approval code is bound to exactly one `request_id`. Replying with the right code on the wrong request is rejected with `error: code does not match this request`.
- **T8.** Approval code expires 10 minutes after the SMS was sent. Replies after the expiry window are rejected with `error: approval code expired`.
- **T9.** Approval code is single-use: a successful approval consumes it; a second reply with the same code (whether approve or deny) is rejected with `error: code already consumed`.
- **T10.** Wrong-code attempts do not extend the expiry window. After 5 wrong-code attempts on the same request, the code is locked (`error: too many wrong attempts; request must be resubmitted`).
- **T11.** Reply parsing is case-insensitive: `yes 123456`, `YES 123456`, `Yes 123456` all approve; `no 123456` denies; anything else (e.g. `maybe 123456`, `123456`, `yes`, garbage) is rejected with a helpful SMS reply guiding the operator.
- **T12.** A reply that arrives while the request is no longer `pending` (e.g. operator already approved via `request-sudoctl approve` locally) is rejected gracefully and the operator gets an SMS back explaining "this request was already approved by another channel."

### Phone routing (default, no callback)

- **T13.** Each approver in `approvers.yaml` has a `phones` list (e.g. `phones: ["+15551234"]`). Default SMS recipients for a request are the union of phones for every UID in the request's `approver_set`.
- **T14.** A request whose `approver_set` is `operators=[aditya, dominic]` with phones `+15551001` and `+15551002` results in the adapter sending SMS to **both** numbers (matches moltpod's "send to primary + secondary" pattern).
- **T15.** An approver UID with no `phones` entry is silently skipped in routing (logged as a warning, not an error).
- **T16.** If the union of all candidate phones is empty, the adapter does not attempt to send SMS, falls back to local-only approval, and emits a `sms_routing_empty` event in canon.

### Routing callback hook (optional)

- **T17.** When `sms_routing_callback_url` is configured, the adapter POSTs `{request_id, requester_uid, default_recipients}` to that URL with an `Authorization: Bearer <secret>` header (from a separately configured shared secret).
- **T18.** Callback URL responds within 500ms with `{recipients: [phones], decision: "default" | "override", reason: "..."}`. Adapter uses the returned `recipients`.
- **T19.** Callback URL times out (>500ms): adapter falls back to `default_recipients` and emits `sms_routing_callback_timeout` event.
- **T20.** Callback URL returns non-200: adapter falls back to `default_recipients` and emits `sms_routing_callback_error` event with the HTTP status.
- **T21.** Callback URL is unreachable (connection refused, DNS failure): adapter falls back to `default_recipients` and emits `sms_routing_callback_unreachable` event.
- **T22.** Callback URL returns a malformed response (not JSON, missing `recipients`, etc.): adapter falls back to `default_recipients` and emits `sms_routing_callback_invalid` event.
- **T23.** Callback URL not configured (the common open-source case): adapter never makes the callback; uses `default_recipients` directly; no event needed.
- **T24.** Audit row in canon for every SMS sent includes `routing_decision: "default" | "callback"`, `default_recipients`, `actual_recipients`, plus the callback's `reason` field if present (so an admin-impersonation override is auditable).

### Twilio rate-limit handling (informed by APPSEC T255 incident)

- **T25.** Twilio API returns 429 (any error code, e.g. `20429`, `60202`, `60203`): adapter does NOT silently retry. Adapter emits `twilio_rate_limited` event in canon with the Twilio error code, the recipient phone (masked), and the rolling-window guidance from Twilio's response.
- **T26.** When rate-limited, adapter returns an immediate failure to the broker. Broker transitions the request to a special `awaiting_local_approval` state (not `failed`), surfaces the rate-limit reason to the requester, and the operator can recover via `request-sudoctl approve` locally.
- **T27.** A rate-limited request is NOT auto-retried by the adapter or the broker. Recovery is operator-driven (either wait for the Twilio window to clear and the operator re-runs `request-sudoctl resend-sms <request_id>`, or the operator approves locally via `request-sudoctl approve`).
- **T28.** `request-sudoctl resend-sms <request_id>` re-attempts the Twilio send; if it succeeds, the request returns to `pending`; if it 429s again, stays in `awaiting_local_approval`.

### Twilio credentials and operational

- **T29.** Twilio credentials (Account SID, Auth Token, Verify Service SID, Sender phone number) live in `/etc/request-sudo/twilio.env`, mode `0600`, owned by the adapter's service user. openclaw cannot read it.
- **T30.** Adapter loads credentials at startup; missing or unreadable file is a hard startup failure with a clear log message.
- **T31.** Twilio Auth Token rotation: adapter reloads credentials on SIGHUP; in-flight verification SIDs created with the old token continue to validate against the old token in memory for their remaining lifetime (so rotation does not invalidate pending approvals).

### Audit (canon events emitted by the adapter)

- **T32.** Adapter emits `sms_sent` event with: `request_id`, recipient phones (masked `+1***1234`), approval-code hash (NOT the raw code), Twilio message SID, timestamp.
- **T33.** Adapter emits `sms_reply_received` event with: `request_id`, sender phone (masked), parsed decision (`approve`/`deny`/`malformed`), code-match result, Twilio message SID.
- **T34.** Adapter emits `sms_approval_consumed` event when a valid `yes <code>` reply leads to `review.approve` being called on the broker.
- **T35.** All masked phones use the same format: country code visible + last 4 digits visible, middle digits replaced with `***`.

## Decision

### Adapter as a separate process

`request-sudo-twilio-adapter` runs as a dedicated service user `request-sudo-adapter` (not root). It has:
- Read access to `/etc/request-sudo/twilio.env` (its own credentials)
- Read access to `/etc/request-sudo/approvers.yaml` (the policy file)
- Network access to Twilio's API (outbound)
- Inbound listener for the Twilio webhook on a localhost-only port (nginx terminates TLS and proxies)
- Connect access to the broker's review socket `/run/request-sudo/review.sock`

The broker authenticates the adapter via `SO_PEERCRED` on the review socket — the adapter's UID must be in the policy file's `review_socket_callers` list.

### Approval-code lifecycle

| Property | Value |
|---|---|
| Format | 6 numeric digits, generated via `crypto/rand` |
| Expiry | 10 minutes from SMS-send timestamp |
| Use | Single-use; first valid reply consumes it |
| Binding | Bound to exactly one `request_id`; code on wrong request is rejected |
| Wrong-attempt limit | 5 attempts then locked; request must be resubmitted |
| Reply format | `yes <code>` to approve, `no <code>` to deny; case-insensitive |

### Routing

Default: each approver in `approvers.yaml` has a `phones: ["+15551234", ...]` array. SMS goes to the union of phones for every UID in the request's `approver_set`.

Optional hook: if `sms_routing_callback_url` is configured, the adapter POSTs the request context to that URL and uses the returned recipients (falling back to default on any failure). The callback is authenticated via a shared bearer token (`SMS_ROUTING_CALLBACK_SECRET`) read from a root-owned env file.

For the moltpod deployment, the callback URL points at moltpod backend's `/api/internal/sms-routing` endpoint, which reads moltpod's impersonation context (per the JWT `impersonating` claim audited in the ADR-0005 prep work) and returns the impersonating-admin's phones when relevant. This keeps the OSS adapter generic — the impersonation logic stays inside moltpod's deployment glue.

### Rate-limit handling

Informed by APPSEC T255 (frontend retry storm that locked out a real user for 10+ minutes via Twilio's `60202`/`60203` per-number caps):

When Twilio returns 429:
1. Adapter emits `twilio_rate_limited` event with the exact Twilio error code.
2. Broker moves the request to `awaiting_local_approval` (a new sub-state of `pending` introduced by this ADR; see "STATE_MACHINE.md amendment" below).
3. Operator recovers via `request-sudoctl approve <id>` locally OR `request-sudoctl resend-sms <id>` after the Twilio window clears.
4. Neither broker nor adapter ever silently auto-retries.

### Twilio webhook authentication

Twilio's documented `X-Twilio-Signature` (HMAC-SHA1 over the full request URL + sorted form params, signed with the Twilio auth token). Constant-time comparison. Signed URL prefix must match the adapter's configured public URL (so a captured signature can't be replayed against a different host).

### Credentials

`/etc/request-sudo/twilio.env`:
```
TWILIO_ACCOUNT_SID=...
TWILIO_AUTH_TOKEN=...
TWILIO_VERIFY_SERVICE_SID=...
TWILIO_FROM_NUMBER=+15551001
SMS_ROUTING_CALLBACK_URL=http://localhost:9810/api/internal/sms-routing   # optional
SMS_ROUTING_CALLBACK_SECRET=...                                            # optional, required if URL set
```

Mode `0600`, owned `request-sudo-adapter:request-sudo-adapter`. Reloaded on SIGHUP.

### STATE_MACHINE.md amendment

Add a sub-state `awaiting_local_approval` under `pending`. Transitions:
- `pending → awaiting_local_approval` when SMS send fails with rate-limit
- `awaiting_local_approval → pending` on successful `request-sudoctl resend-sms`
- `awaiting_local_approval → approved` on `request-sudoctl approve` (local fallback)
- `awaiting_local_approval → expired` on request TTL (5 min per current STATE_MACHINE §6; consider extending TTL when in this sub-state — open question)

## Considered options

- **Mattermost as the v1 channel** (rejected): Mattermost runs on the same box as the requester (openclaw + broker), making it not truly out-of-band. A real out-of-band channel must survive box compromise.
- **Voice call via Twilio Voice** (deferred): more reliable in poor-cell areas; not needed for v1.
- **Push notification via Pushover/Pushbullet** (deferred): adds a per-operator device-registration step.
- **Email** (deferred): slower delivery; harder to gate against MITM without signing.
- **5-minute approval-code expiry** (rejected per user feedback 2026-05-11): too short — operator typically goes from desk to phone, unlocks, navigates, reads, types. 10 minutes matches Twilio Verify's default and user's stated comfort.
- **Alphanumeric A7K-style code** (rejected): harder to type on phone keypad. 6 numeric digits matches moltpod's existing UX and Twilio Verify defaults.
- **Per-request SMS opt-in flag** (rejected): operator decides routing once in the policy file, not per-request. The agent (openclaw) shouldn't be trusted to flip the channel-of-record per request.
- **Embedded Starlark for routing hook** (rejected as overengineering): HTTP callback to a configured URL is simpler, more portable, and lets the moltpod-specific impersonation logic live inside moltpod's own codebase.
- **Auto-retry on Twilio 429** (rejected; APPSEC T255 evidence): silently retrying compounds the lockout. Operator-driven recovery is the only sane behavior.

## Consequences

- The adapter is a real new service to install, monitor, and update alongside `request-sudod`. The installer must register both.
- Twilio account costs apply: ~$0.0075 per outbound SMS, ~$0.0075 per inbound webhook. At sudo request volumes (a few per day) the bill is negligible (<$1/month per deployment).
- The optional callback hook means deployments that want admin-impersonation override (moltpod) can implement it server-side without the OSS adapter caring. Public OSS users who just want the simple per-approver-phones model never set the callback URL.
- The `awaiting_local_approval` sub-state is a small protocol addition. Surfacing it cleanly in CLI output (`request-sudo status`) and in `request-sudoctl pending` is now a UX requirement.
- The rate-limit-handling design means a Twilio outage degrades request-sudo gracefully: SMSes don't get sent, but local approval (`request-sudoctl approve`) still works on the box. Operators are not locked out by upstream Twilio problems.
- Phone-number rotation: when an operator's phone changes, the operator updates `approvers.yaml` and SIGHUPs the broker. No restart.
- The Twilio Verify Service SID is reused for the broker's full lifetime — this means hitting the `60202` Verify-SID rate limit globally locks the broker's SMS path. **Possible follow-up**: rotate Verify Service SID per request to avoid this single-point-of-failure. Deferred — would require more Twilio API understanding and likely a Twilio support conversation about how their Verify Services are billed.
- Adapter does not implement `target_user` or `encrypted-to-approvers` — those are still reserved-but-rejected (ADRs 0003 and 0004).

## Amendment — ADR-0005a — Verify-as-transport with serialization invariant (2026-05-19)

Status: accepted, supersedes T6/T7/T11 of v1; amends T9/T10; adds T34/T35/T36.

### Why the reversal (empirical-finding-driven)

Two outbound SMS paths were tested against the moltpod prod Twilio account on 2026-05-14 and re-tested 2026-05-18:

- **Twilio Messages API to US numbers from unregistered 10DLC senders fails.** Returns Twilio error code `30034`. Carrier silently drops the message under A2P 10DLC enforcement. Verified twice empirically against `+13102957704`. Same payload, same creds, message never delivered.
- **Twilio Verify API with `CustomFriendlyName` (any value, including the literal string `"moltpod"`) rejected with error `60204` "Custom friendly name not allowed"** on the moltpod prod Verify Service. The adapter cannot depend on `CustomFriendlyName` being permitted for an arbitrary OSS operator's Verify Service.
- **Twilio Verify API with only `To` + `Channel=sms` (default Verify template) succeeded.** Real SMS delivered as `"Your <Verify Service friendly_name> verification code is: NNNNNN"`. Same Twilio account, same creds, different sending path.

Conclusion: the v1 design (adapter generates the code, sends via Messages API, parses `yes <code>` against an adapter-stored code) is non-deliverable on US numbers without A2P 10DLC registration, which is a multi-week operator-side hurdle that violates the 5-min portability promise. Verify-default is deliverable today, on any Twilio account, with zero Console customization.

### T-clause amendments

| Clause | v1 | v1a |
|---|---|---|
| **T6 (transport)** | adapter-generated 6-digit code via Twilio Messages API | Twilio Verify API default template; Twilio owns code entropy and generation |
| **T7 (binding authority)** | adapter-generated token in SMS body binds reply to request | serialization invariant — at most one pending Verify per `recipient_phone` within Verify session TTL (10 min); reply unambiguously resolves to that request because there is exactly one |
| **T8 (code expiry 10-min)** | unchanged | unchanged — Twilio Verify session TTL is also 10 min, semantics match |
| **T9 (single-use)** | adapter-enforced | now server-side enforced by Twilio's VerificationCheck; Twilio rejects re-use after first `approved` |
| **T10 (5-wrong-attempt lockout)** | adapter-enforced | authority shifts to Twilio's Verify Service `max_attempts` (default 5); operators configure this in the Twilio Console at Service creation |
| **T11 (reply parsing)** | parse `yes <code>` / `no <code>` against adapter-stored code | extract digits from reply body, resolve sender phone to the single pending request via the serialization-invariant set, call Twilio VerificationCheck; on `approved` → broker `review.approve`; on `no <code>` → broker `review.deny` locally without contacting Twilio |
| **T32 (out-of-band requirement)** | unchanged | unchanged; POLL_MODE explicitly does NOT satisfy this and is build-tag-gated (`//go:build dev_only`) |

### New clauses

- **T34 — Serialization invariant.** At most one entry per `recipient_phone` in the adapter's in-memory pending set, with TTL = 10 min matching Twilio Verify session TTL. Lazy auto-release on TTL expiry emits `verify_pending_expired` adapter-audit event. Same-phone duplicates within TTL are routed to `awaiting_local_approval` state (sub-state of `pending`, per v1). On adapter restart, the set is reconstructed by replaying the adapter-audit JSONL.
- **T35 — OSS portability config.** Operator config requires `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_VERIFY_SERVICE_SID` only. No Twilio Console template customization required. Adapter does NOT pass `CustomFriendlyName` or `CustomCode`.
- **T36 — Country-code allowlist.** Adapter accepts `--allowed-country-codes` CLI flag (comma-separated E.164 country prefixes, default `+1`). Numbers outside the allowlist emit `unsupported_region` adapter-audit event and never reach Twilio. Request stays pending; local approve via `request-sudoctl approve <id>` still works (dual-mode invariant preserved).

### Why this preserves the "adapter owns approval semantics" rationale from v1

The original v1 rationale for adapter-owned codes was to prevent Twilio-side incidents (APPSEC T255-style retry storms, leaked Twilio tokens, Verify-Service-SID rate-limit lockouts forging approvals) from minting approvals on requests they shouldn't. Serialization preserves this property:

- Twilio's code can only `approved`-check the **one** pending request bound to that phone slot. A leaked Verify code only approves the one pending request that's currently pending for that phone — not a future request, not a different request.
- Replay-after-approval is server-side-rejected by Twilio's VerificationCheck (T9 still holds; authority moved server-side).
- v1 T26 fallback path (`awaiting_local_approval` for Twilio-down or rate-limited) is preserved verbatim. Operator can `request-sudoctl approve <id>` locally without ever touching Twilio. T36 country-allowlist drops also land in this state.

### Deferred items (additions to v1's "Considered options")

- **CustomFriendlyName argv hint** — revisit when the operator's Verify Service is configured to allow it. Some OSS operator Twilio Services may permit it; the moltpod prod Service does not. See `docs/twilio-setup.md`.
- **A2P 10DLC registration for Messages-API fallback** — revisit if the Verify path becomes inadequate for an operator's volume. Multi-week brand/campaign registration; not on the v1a path.

### Cross-references

- `docs/twilio-setup.md` — operator runbook (5-min portability promise, deliverability tiers, troubleshooting)
- `tests/dualmode/dualmode_test.go` — release-blocker invariant for the dual-mode (Twilio + local) guarantee
- `internal/twilioadapter/pending_set.go` — serialization-invariant implementation (T34)
- `internal/twilioadapter/country_guard.go` — country-code allowlist enforcement (T36)

