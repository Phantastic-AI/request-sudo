# request-sudo — MVP Plan

_Last updated: 2026-04-20 UTC_

## The decision
Start fresh and keep request-sudo as the canonical product name.

Use the older plugin repo only as reference.

## The MVP

The MVP should deliver:
- simple request UX
- readable approval UX
- exact one-time privileged execution
- local manual approval first
- immutable event history
- installer path that works

## Core shape

### Requester
Uses `request-sudo`.
Gets back a request ID and status, not root.

### Approver
Uses `request-sudoctl` locally in the first slice.
Remote transport can come later.

### Broker
`request-sudod` is the only privileged writer/executor.

## Current implementation phases

### Phase 1 — protocol and skeleton
Done.

### Phase 2 — local runnable slice
Done.

### Phase 3 — hardening + installer
Current focus:
- replay tamper verification
- review-socket auth tightening
- automated smoke path
- installer + systemd unit

### Phase 4 — future transport work
Possible future work:
- SMS/chat approval flow with short case-insensitive approval tokens such as `A7K`
- queue summaries and delayed backlog notices
- transport-specific approval UX

## Verification

The repo is in good shape when all of these pass together:
- `go test ./...`
- `go build ./...`
- `go vet ./...`
- `./scripts/verify-contracts.sh`
- installer verification
- automated local smoke path

## Remaining follow-up risks
- tamper evidence should eventually be enforced everywhere it matters
- review policy may need stronger role modeling over time
- transport approval flow still needs a dedicated phase after local-first stability is proven
