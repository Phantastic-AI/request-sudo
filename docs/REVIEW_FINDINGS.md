# Phase 1 review findings

_Last updated: 2026-04-20 UTC_

This document records the current code-quality and delivery findings for the successor workspace.
It is intentionally narrow: review the current phase-1 shape against the frozen design, then call out the highest-value follow-up gaps.

## What is already in good shape

### Design freeze is explicit

The workspace has a clear frozen-design set:

- `SPEC.md`
- `PLAN.md`
- `ARCHITECTURE_OUTLINE.md`
- `PROTOCOL.md`
- `STATE_MACHINE.md`

Those documents agree on the main trust boundaries:

- broker is the only privileged executor
- request and review traffic use separate Unix sockets
- approval binds to exact execution context
- durable state comes from an append-only event log
- projection is rebuildable and must never become a hidden source of truth

### Phase-1 Go skeleton now exists

The workspace now contains the expected implementation skeleton:

```text
cmd/
internal/
```

The current implementation covers the phase-1 center of gravity:

- separate requester and review Unix sockets
- single-writer broker service
- append-only JSONL event log with per-entry hash chaining
- rebuildable in-memory projection
- submit / status / approve / deny / execute flow
- crash-recovery handling for requests left in `executing`

That means the workspace is no longer docs-only; there is now a concrete runtime slice to review.

### Contract-test scaffold already exists

The workspace already contains a useful contract-test skeleton:

- `tests/contracts/`
- `scripts/verify-contracts.sh`

That scaffold already protects several design truths:

- requester contract JSON shapes
- one-time execution state transitions
- local manual-review smoke path
- non-goals such as plugin-first execution paths

### Documentation lane is now coherent

The docs set now includes:

- `docs/PHASE1_REVIEW.md`
- `docs/SMOKE_PATH.md`
- `docs/VERIFICATION.md`
- this file: `docs/REVIEW_FINDINGS.md`

Together they cover review scope, smoke path, verification order, and current gaps.

## Current gaps

### Gap 1: replay does not verify tamper evidence yet

`internal/events/log.go` writes hash-chained events, but `Replay()` currently decodes lines without recomputing and validating:

- each event hash
- each `prev_hash` link

Impact:

- the log is hash-chained, but tamper evidence is not enforced during replay
- a corrupted or edited log could be accepted unless a separate verifier is added

### Gap 2: review-socket authorization is narrower than the frozen protocol

`internal/socket/server.go` correctly splits request and review lanes and checks peer UID on the review socket.
However, `PROTOCOL.md` describes review-path checks in terms of:

- peer uid
- peer gid
- service-user allowlist or root

Current code enforces only a UID allowlist.

Impact:

- the trust boundary exists, but it is still slimmer than the protocol contract
- future hardening should decide whether GID- and role-based review checks are required for phase 1 or phase 2

### Gap 3: automated verification still trails the available runtime slice

The workspace now has real Go code plus focused unit tests, but the default docs still rely heavily on:

- contract-fixture checks
- package tests
- manually described smoke evidence

Impact:

- the implementation is demonstrable, but the repeatable verification path could be tighter
- adding an automated smoke script around temporary sockets/runtime paths would reduce drift between docs and executable proof

### Gap 4: review coverage is stronger on happy-path mechanics than on adversarial cases

Current tests and fixtures cover core lifecycle behavior well, but the most security-sensitive follow-up checks should keep expanding around:

- review-socket spoofing attempts
- tampered event-log replay
- denial / revoke / recovery edge cases across restart boundaries
- mismatch between documented trust rules and enforced runtime checks

## Recommended next actions

1. Validate the event hash chain during replay, not only during append.
2. Decide whether phase 1 should enforce review-socket GID/service-user rules in addition to UID allowlisting.
3. Add an automated smoke runner for the local manual approval path against a temporary runtime directory.
4. Expand tests around tampered logs, denied/rejected paths, and recovery edge cases.

## Review verdict

Current phase-1 documentation, runtime skeleton, and verification scaffolding are aligned on the main product shape.
The main remaining risks are now hardening and verification-depth risks rather than missing-foundation risks:

- replay does not yet verify tamper evidence
- review-socket enforcement is narrower than the full protocol wording
- automated smoke coverage can be stronger

At this point the lane should be treated as **runtime demonstrated, but security/verification hardening still in progress**.
