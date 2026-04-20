# Phase 1 review findings

_Last updated: 2026-04-20 UTC_

This document records the current code-quality and delivery findings for the successor workspace.
It is intentionally narrow: review the current phase-1 shape against the frozen design, then call out gaps that should block or guide the next implementation pass.

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

### Gap 1: phase-1 Go skeleton is not in this workspace yet

The frozen design calls for Go requester/daemon skeletons and internal packages.
At review time, the workspace still does not contain the expected Go source tree such as:

```text
cmd/
internal/
```

Impact:

- no actual broker core exists yet in this workspace
- smoke-path docs are specification-only today
- verification currently proves contract fixtures, not the broker runtime

### Gap 2: no root git repository is present

`/srv/moltpod/security/lease-broker-successor` is not currently a git repo.

Impact:

- worker commit protocol cannot be completed here
- task completion evidence can be produced, but the required commit artifact cannot
- integration will need either repo initialization or migration into the intended tracked repo

This is a delivery blocker, not a design blocker.

### Gap 3: current verification is contract-first, not runtime-first

`./scripts/verify-contracts.sh` is a useful baseline, but it currently validates only the fixture/test scaffold under `tests/contracts/`.

Impact:

- it does not yet prove socket binding, peer-credential capture, append-only writes, or one-time execution against real code
- runtime verification must expand once `cmd/lb` and `cmd/lbd` exist

### Gap 4: review socket trust rules are specified but not implemented here

`PROTOCOL.md` clearly separates `/run/lb/request.sock` and `/run/lb/review.sock`.
Until runtime code lands, there is no enforcement evidence for:

- requester peer separation
- approver allowlist behavior
- prevention of review-path spoofing from the requester side

## Recommended next actions

1. Land the Go skeleton in this workspace with separate request/review listeners.
2. Implement append-only event writing before adding remote transports.
3. Rebuild projection strictly from event replay and add tests for recovery from `executing`.
4. Extend `scripts/verify-contracts.sh` or add a sibling script once runtime code exists so verification covers:
   - `go test ./...`
   - `go test -race ./...`
   - `go vet ./...`
   - a temporary-dir smoke run for the local manual approval path
5. Fix the repository bootstrap issue so worker outputs can be committed under the required lore protocol.

## Review verdict

Current phase-1 documentation and contract scaffolding are directionally sound and aligned with the frozen design.
The main remaining risks are execution risks rather than design risks:

- missing Go implementation in this workspace
- missing runtime verification
- missing git repository for required task commits

Until those gaps close, this lane should be treated as **docs-and-contracts ready, runtime not yet demonstrated**.
