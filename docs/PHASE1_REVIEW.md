# Phase 1 build review

_Last updated: 2026-04-20 UTC_

## Frozen design inputs

Phase 1 must stay aligned with these documents:

- `SPEC.md`
- `PLAN.md`
- `ARCHITECTURE_OUTLINE.md`
- `PROTOCOL.md`
- `STATE_MACHINE.md`

If code or tests disagree with those files, update the implementation rather than silently drifting the design.

## Scope for phase 1

Build the smallest concrete slice that proves the successor shape:

1. Go requester and daemon skeletons
2. Unix socket IPC skeleton on separate request/review sockets
3. append-only event log with hash chaining
4. rebuildable projection from the canonical log
5. local manual approval path only
6. smoke-path and verification scaffolding

## Guardrails

### Single writer

`request-sudod` is the only writer for durable state.

That means:

- request creation writes events through the daemon
- approval and denial write events through the daemon
- execution start and terminal events write through the daemon
- recovery writes any repair event through the daemon

No sidecar helper should mutate projection files directly.

### Exact execution

The requester-submitted `argv` is the execution truth.
Human-readable summaries may be rewritten for clarity, but the executed command must stay byte-for-byte tied to the approved digest.

### Two sockets, two trust levels

Phase 1 should keep these boundaries explicit:

- `/run/request-sudo/request.sock` for low-privilege callers
- `/run/request-sudo/review.sock` for trusted review tooling

Do not collapse both paths into one listener during the skeleton stage.

### No plugin-first path

The sibling `../openclaw-lease-broker` repo is reference material only.
Phase 1 should not center plugin hooks, passive interception, or current Airlock request flow.

## Recommended package shape

This is a review target, not a frozen source tree. Keep names close unless there is a strong reason to differ.

```text
cmd/
  request-sudo/
  request-sudoctl/
  request-sudod/
internal/
  approval/
  events/
  ipc/
  projection/
  requests/
  review/
  state/
  system/
```

Review each package against one question: does it preserve the broker as the only privileged writer and executor?

## Review checklist for worker lanes

### Broker + IPC lane

- requester socket and review socket are distinct
- peer identity is captured from the Unix socket layer
- `request.submit`, `request.status`, and `request.execute` shapes match `PROTOCOL.md`
- request IDs are stable and opaque
- execution is blocked unless state is exactly `approved`

### Event log + projection lane

- every state transition is sourced from an event
- event entries contain previous-hash linkage
- projection can be rebuilt from the log without extra hidden state
- replay does not re-execute commands
- `executing` is represented explicitly

### Docs + verification lane

- README points contributors to the frozen design set
- smoke-path is written before remote transport work exists
- verification commands prefer built-in Go tooling first (`gofmt`, `go test`, `go test -race`, `go vet`)
- docs call out that local manual review is the only required approval UX in phase 1

## Exit criteria for a review pass

A phase-1 review is in good shape when all of the following are true:

- request lifecycle matches `STATE_MACHINE.md`
- audit events match the minimum event list in `STATE_MACHINE.md`
- IPC contract matches `PROTOCOL.md`
- no implementation path grants broad reusable privilege to the requester
- the smoke path can be described end-to-end without hand-waving over trust boundaries
