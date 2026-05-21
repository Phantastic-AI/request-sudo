# request-sudo

> **For agent operators who want their LLM-driven automation to ask before running anything privileged.**
>
> Your AI agent submits an exact command. A human sees a clear explanation. The exact command runs once if approved — and nothing else.

[![status](https://img.shields.io/badge/status-v1-blue)]() [![tests](https://img.shields.io/badge/tests-green-success)]()

## The problem

You let an LLM-driven agent run on your machine. Most of what it does is fine. But every so often it wants to:

- restart a daemon
- `rm -rf` something it shouldn't
- run a migration that takes the database offline for two minutes
- `chmod 777` a directory because that "fixed" the permission error it didn't understand
- run an `apt install` that pulls in 400 MB of who-knows-what

Today your two options are bad:

1. **Give the agent passwordless `sudo`.** It runs anything, any time, with no human in the loop. Your audit trail is "an agent did it." Good luck.
2. **Take privileged operations away entirely.** Now you're the bottleneck. Your agent files a Linear ticket and waits for you to read it.

`request-sudo` is the middle path: **the agent asks, you see exactly what it wants to do, you approve or deny that specific thing, it runs once and only once.**

## How it feels

Inside your agent's shell — no privilege:

```bash
$ request-sudo request --reason "rotate the nginx logs" -- /bin/systemctl restart nginx
{
  "request_id": "req_a000680c17d6",
  "status": "pending",
  "summary": {
    "requester": "claude-code",
    "exact_command": "/bin/systemctl restart nginx",
    "reason": "rotate the nginx logs",
    "effect": "run a privileged command"
  }
}
```

On your phone, seconds later:

```
Your Acme verification code is: 482910
```

You open `request-sudoctl pending` on the box (or check the wall(1) broadcast already on every operator TTY), see the exact argv + reason, and either text back `482910` to approve or `no 482910` to deny. The daemon runs the *exact* approved command — same argv, sanitized environment, frozen at submit time — once. Then it's done. The approval can't be reused, can't be widened, can't be replayed.

If your agent later wants to do something different, that's a new request. Same flow.

## Why the design is shaped this way

Most privilege boundaries on Linux were designed for humans authorizing humans:

- `sudoers` patterns approve *classes of commands forever*. You approve `systemctl restart *` once, and now your agent can restart anything until you remember to revoke it.
- Standing root shells expose every system call.
- Most audit tools tell you *what ran*, not *who approved it and on what specific argv*.

When your "user" is an LLM, those failure modes get sharper:

- The agent has no concept of "stop and reflect" — it acts on the next token. If the door is open, it walks through.
- Patterns get matched literally. `bash -c` slips around any argv filter you write.
- A leaked agent credential isn't a "compromised user" — it's an army of compromised users that don't sleep.

`request-sudo`'s answer is **one-request-one-approval-one-execution**:

1. The agent submits an exact argv via `request-sudo`. No argv globs, no policy templates.
2. The root-owned daemon (`request-sudod`) computes a canonical digest over `(argv, sanitized PATH/env, requester uid, request id, expiry)` and writes a `request_created` event to an append-only, hash-chained log.
3. A human (or trusted out-of-band service) approves the *frozen* digest. Approvals bind to bytes-for-bytes frozen values — not patterns, not regexes, not classes.
4. The daemon executes the approved argv once, in a sanitized environment, and writes the outcome to the log.

The trust boundary is the literate approval flow. **You approve commands you understand**, not policies you hope will hold.

Argv is deliberately opaque — the broker doesn't parse, filter, or sanitize what your agent submitted. Filtering creates false confidence; the human reading the literate summary is the trust boundary, full stop. If your agent asks to run `["bash", "-c", "rm -rf /tmp/canary"]`, that's what you see, and that's what runs if you approve.

## Quick start

```bash
git clone https://github.com/Phantastic-AI/request-sudo
cd request-sudo
go build ./...

# Local sandbox smoke test (no install, no Twilio):
./scripts/smoke-local.sh
```

For a real install with systemd, see [`packaging/systemd/`](packaging/systemd/) and the install paths under `/usr/local/bin/` + `/etc/request-sudo/`.

## SMS approvals (optional)

The agent submits requests from anywhere — but you want to be able to approve from your phone, away from the box, in a way that survives box compromise. `request-sudo` ships an additive Twilio adapter for SMS approvals.

**5 minutes to set up, 3 env vars, no paperwork:**

```bash
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_VERIFY_SERVICE_SID=VAxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

No A2P 10DLC registration, no custom-template approval cycle, no Twilio sales contact. The adapter routes via Twilio Verify's pre-registered sender pool, which bypasses US carriers' 10DLC filtering that drops unregistered Messages-API traffic. See [`docs/twilio-setup.md`](docs/twilio-setup.md) for the operator runbook, honest deliverability tiers, and troubleshooting.

The adapter is **strictly additive** — `request-sudod` runs end-to-end without it, without a `twilio.env` file, and without any `phones:` entries in `approvers.yaml`. That release-blocker invariant is covered by [`tests/dualmode/`](tests/dualmode/dualmode_test.go).

## Architecture at a glance

```
                                    submit / status / execute
   ┌─────────────────┐  request.sock  ┌──────────────┐
   │  request-sudo   │ ─────────────▶ │              │
   │ (agent / user)  │                │              │
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

- **Single-writer rule:** only `request-sudod` appends to the canonical event log. The agent CLI, approver CLI, and Twilio adapter communicate via sockets; they never touch canon directly.
- **Two-socket split:** `request.sock` (low-privilege submitters) and `review.sock` (trusted approver tooling) are distinct file paths with distinct ACLs. The broker enforces approver-not-requester at the protocol level — your agent cannot approve its own requests.
- **Canonical digest:** every request freezes a hash over the exact argv + sanitized PATH/env + requester uid + request id + expiry at submit time. Approvals bind to that frozen digest; execute uses bytes-for-bytes frozen values. Runtime drift never produces digest mismatches.
- **Serialization invariant:** at most one pending Verify per recipient phone within the 10-min Verify session TTL — so an operator's reply unambiguously resolves to a single request even if two were fired at the same phone.

## Design docs

- [`CONTEXT.md`](CONTEXT.md) — domain glossary (Request, Approval, Approver, canon, projection, sockets…)
- [`DECISIONS.md`](DECISIONS.md) — index of accepted ADRs, deferred items, and open questions
- [`docs/adr/`](docs/adr/):
  - [ADR-0001](docs/adr/0001-drop-lease-terminology.md) — drop "lease" terminology
  - [ADR-0002](docs/adr/0002-static-file-approver-routing.md) — static-file approver routing
  - [ADR-0003](docs/adr/0003-sanitized-execution-context.md) — sanitized execution context
  - [ADR-0004](docs/adr/0004-canon-storage-format-and-integrity-guarantees.md) — canon storage + integrity guarantees
  - [ADR-0005 + ADR-0005a](docs/adr/0005-twilio-sms-adapter-trust-and-lifecycle.md) — Twilio adapter trust model + Verify-as-transport amendment
- [`PROTOCOL.md`](PROTOCOL.md) — wire protocol for the request + review sockets
- [`STATE_MACHINE.md`](STATE_MACHINE.md) — request lifecycle states + transitions
- [`AGENTS.md`](AGENTS.md) — instructions for AI agents working in this repo

## Status

**v1** — local-first slice ships and is in field use; SMS adapter ships with Verify-as-transport per ADR-0005a. Webhook reply roundtrip requires a publicly-reachable URL (ngrok / nginx); local fallback via `request-sudoctl approve <id> --code <sms-code>` works without it.

## Non-goals (v1)

- **Cross-machine RPC.** `request-sudo` runs privileged actions on the box the daemon is on. A different broker (separate repo) will handle "run X on host Y" — see [ADR-0001](docs/adr/0001-drop-lease-terminology.md).
- **Reusable leases / approval pools.** Approvals are one-request-one-execution. The old OpenClaw lease broker plugin was conceptually a lease broker; request-sudo is its **successor in role, not in semantics.**
- **Filter-based argv approval.** The broker does not parse, sanitize, or filter argv. The literate approval flow is the trust boundary; if a human approves `["bash", "-c", "rm -rf /tmp/canary"]`, that's what runs.

## License

TBD — open an issue if licensing matters for your use case.
