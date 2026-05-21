# request-sudo

> A literate permission broker for Linux. A user or agent asks for one privileged action; a human sees a clear explanation; the exact action runs once if approved.

[![status](https://img.shields.io/badge/status-v1-blue)]() [![tests](https://img.shields.io/badge/tests-green-success)]()

## What it is

`request-sudo` replaces ad-hoc `sudoers` rules and standing root sessions with **one-request-one-approval-one-execution** semantics:

1. A low-privilege user or agent submits an exact command via `request-sudo`.
2. A trusted root-owned daemon (`request-sudod`) computes a canonical digest, records the request to an append-only event log, and notifies approvers.
3. An approver — human or trusted service — issues `request-sudoctl approve <id>` (or replies to an SMS) on a separate review socket.
4. The daemon executes the *exact* approved argv once, in a sanitized environment, and writes the outcome to the log.

The trust boundary is the literate approval flow itself: humans approve commands they understand, not policies they hope work. Approvals are single-use and bound to a frozen digest of (argv, sanitized environment, requester uid). They cannot be reused, widened, or replayed.

## Why

Most Linux privilege boundaries are coarse: `sudoers` patterns approve *classes* of commands forever; standing root shells expose every system call. `request-sudo` instead approves one specific argv, once, with full context. Designed for:

- **Agent operators** who want their LLM-driven automation to ask before running anything privileged.
- **Ops teams** moving off shared `root` access without giving up scriptability.
- **Compliance contexts** that need a cryptographically-chained audit log of every privileged action and who approved it.

## Quick start

```bash
git clone https://github.com/Phantastic-AI/request-sudo
cd request-sudo
go build ./...

# Sandbox smoke test (no install required):
./scripts/smoke-local.sh
```

For a real install with systemd, see [`packaging/systemd/`](packaging/systemd/) and follow the install paths under `/usr/local/bin/` + `/etc/request-sudo/`.

## SMS approvals (optional)

`request-sudo` ships an additive Twilio adapter for out-of-band SMS approvals. A new operator can wire it up in **5 minutes** with three env vars (`TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_VERIFY_SERVICE_SID`) — no A2P 10DLC registration, no custom-template approval cycle, no Twilio sales contact. The adapter routes via Twilio Verify's pre-registered sender pool, which bypasses US carriers' 10DLC filtering that blocks unregistered Messages-API traffic.

See [`docs/twilio-setup.md`](docs/twilio-setup.md) for the runbook, honest deliverability tiers (US-registered / US-unregistered / international), and troubleshooting.

The adapter is **strictly additive** — `request-sudod` runs end-to-end without it, without a `twilio.env` file, and without any `phones:` entries in `approvers.yaml`. This release-blocker invariant is covered by [`tests/dualmode/`](tests/dualmode/dualmode_test.go).

## Architecture at a glance

```
                                    submit / status / execute
   ┌─────────────────┐  request.sock  ┌──────────────┐
   │  request-sudo   │ ─────────────▶ │              │
   │  (user CLI)     │                │              │
   └─────────────────┘                │              │
                                      │  request-    │     append-only
   ┌─────────────────┐  review.sock   │   sudod      │ ──▶ canon
   │  request-       │ ─────────────▶ │  (root)      │     (events.jsonl)
   │   sudoctl       │                │              │
   │  (approver CLI) │                │              │
   └─────────────────┘                └──────────────┘
                                            ▲
   ┌────────────────────┐                   │ review.approve / deny
   │ request-sudo-      │  /twilio/inbound  │
   │  twilio-adapter    │ ◀── webhook ──── Twilio Verify SMS
   │  (unprivileged)    │
   └────────────────────┘
```

- **Single-writer rule:** only `request-sudod` appends to the canonical event log. CLI tools and adapters communicate via sockets; they never write canon directly.
- **Two-socket split:** `request.sock` (low-privilege callers) and `review.sock` (trusted approver tooling) are distinct file paths with distinct ACLs. The broker enforces approver-not-requester at the protocol level.
- **Canonical digest:** every request freezes a hash over `(argv, sanitized PATH/env, requester uid, request id, expiry)` at submit time. Approvals bind to that frozen digest; execute uses bytes-for-bytes frozen values, so binary-upgrade-mid-flight or runtime drift never produce digest mismatches.

## Design docs

- [`CONTEXT.md`](CONTEXT.md) — domain glossary (Request, Approval, Approver, canon, projection, sockets…)
- [`DECISIONS.md`](DECISIONS.md) — index of accepted ADRs, deferred items, and open questions
- [`docs/adr/`](docs/adr/) — accepted architecture decisions:
  - [ADR-0001](docs/adr/0001-drop-lease-terminology.md) — drop "lease" terminology
  - [ADR-0002](docs/adr/0002-static-file-approver-routing.md) — static-file approver routing
  - [ADR-0003](docs/adr/0003-sanitized-execution-context.md) — sanitized execution context
  - [ADR-0004](docs/adr/0004-canon-storage-format-and-integrity-guarantees.md) — canon storage + integrity guarantees
  - [ADR-0005 + ADR-0005a](docs/adr/0005-twilio-sms-adapter-trust-and-lifecycle.md) — Twilio adapter trust model + Verify-as-transport amendment
- [`PROTOCOL.md`](PROTOCOL.md) — wire-protocol for the request + review sockets
- [`STATE_MACHINE.md`](STATE_MACHINE.md) — request lifecycle states + transitions
- [`AGENTS.md`](AGENTS.md) — instructions for AI agents working in this repo

## Status

**v1** — local-first slice ships and is in field use; SMS adapter ships with `Verify`-as-transport (ADR-0005a). Webhook reply roundtrip requires a publicly-reachable URL (ngrok / nginx); local fallback via `request-sudoctl approve <id> --code <sms-code>` works without it.

## Non-goals (v1)

- **Cross-machine RPC.** `request-sudo` runs privileged actions on the box the daemon is on. The (separate) action broker is a different project — see [ADR-0001](docs/adr/0001-drop-lease-terminology.md).
- **Reusable leases.** Approvals are one-request-one-execution. The old OpenClaw lease broker (`openclaw-lease-broker` plugin) was conceptually a lease broker; request-sudo is its **successor in role, not in semantics.**
- **Filter-based argv approval.** The broker does not parse, sanitize, or filter argv. The literate approval flow is the trust boundary; `["bash", "-c", "rm -rf /"]` executes if a human approves it. Argv filtering creates false confidence.

## License

TBD — open an issue if licensing matters for your use case.
