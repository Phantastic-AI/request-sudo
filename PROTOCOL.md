# Lease Broker Successor — Protocol Contracts

_Last updated: 2026-04-20 UTC_

## 1. Contract overview

This document freezes the first concrete contracts for the successor.

It answers:
- what `lb` sends
- what `lbd` returns
- how approval is submitted
- which socket does what
- who is allowed to talk on which path

The point is simple:
- requester writes requests
- broker owns state
- approver path submits decisions
- broker is the single writer and sole executor

---

## 2. IPC transport

Use **Unix domain sockets**.

### Socket paths
- requester socket: `/run/lb/request.sock`
- review socket: `/run/lb/review.sock`

These are filesystem paths, but they are **sockets**, not normal files.

### Why two sockets
Because requester traffic and review traffic have different trust rules.

- requester socket is for low-privilege callers asking for work
- review socket is for trusted approval tooling and local admin tools

This keeps the requester from pretending to be the approver path.

---

## 3. Socket auth rules

### `/run/lb/request.sock`
Allowed callers:
- normal local users
- OpenClaw user
- least-privilege test users

Broker checks:
- peer uid
- peer gid
- peer pid when available

### `/run/lb/review.sock`
Allowed callers:
- `lbctl`
- approved review/adapter service users only

Broker checks:
- peer uid
- peer gid
- service user allowlist or root

### Important rule
The requester socket and review socket must never be treated as equivalent.

---

## 4. Core requester contract

### Request submit

The requester submits exact argv.

Example conceptual call:

```bash
lb request -- systemctl restart app-moltpod-backend
```

### Submit payload

```json
{
  "action": "request.submit",
  "argv": ["systemctl", "restart", "app-moltpod-backend"],
  "reason": "restart after deploy",
  "mode": "poll"
}
```

Notes:
- `argv` is canonical input
- no shell string rewriting
- `mode` is `poll` by default
- optional `mode=wait` is allowed later as convenience

### Submit response

```json
{
  "request_id": "req_123",
  "status": "pending",
  "message": "Approval required"
}
```

What this means:
- request is recorded
- no privileged action has run
- requester has no sudo/root capability

---

## 5. Requester status contract

### Status request

```json
{
  "action": "request.status",
  "request_id": "req_123"
}
```

### Status response examples

Pending:

```json
{
  "request_id": "req_123",
  "status": "pending"
}
```

Approved:

```json
{
  "request_id": "req_123",
  "status": "approved"
}
```

Denied:

```json
{
  "request_id": "req_123",
  "status": "denied",
  "message": "Denied by approver"
}
```

Executed:

```json
{
  "request_id": "req_123",
  "status": "executed",
  "exit_code": 0,
  "stdout": "",
  "stderr": ""
}
```

---

## 6. Requester execute contract

### Execute request

The requester does **not** execute as root.
It asks the broker to execute the already-approved request.

```json
{
  "action": "request.execute",
  "request_id": "req_123"
}
```

### Execute success response

```json
{
  "request_id": "req_123",
  "status": "executed",
  "exit_code": 0,
  "stdout": "",
  "stderr": ""
}
```

### Execute rejection response

```json
{
  "request_id": "req_123",
  "status": "rejected",
  "message": "Request is not approved"
}
```

### Execute idempotency

`request.execute` must never launch a second execve for the same request.

If execute is called again:
- while state is `executing`, return current state only
- after state is `executed` or `failed`, return that terminal state only
- do not run the command a second time

### Important rule
The broker executes the approved argv itself.
The requester never receives general privileged capability.

---

## 7. Review contract

This is the approval path used by `lbctl` and later by adapters.

### Approve request

```json
{
  "action": "review.approve",
  "request_id": "req_123",
  "approver": {
    "kind": "local",
    "id": "root"
  },
  "totp": "482911"
}
```

### Deny request

```json
{
  "action": "review.deny",
  "request_id": "req_123",
  "approver": {
    "kind": "local",
    "id": "root"
  },
  "reason": "Not appropriate right now"
}
```

### Review response

```json
{
  "request_id": "req_123",
  "status": "approved"
}
```

or

```json
{
  "request_id": "req_123",
  "status": "denied"
}
```

---

## 8. TOTP contract

TOTP is not sufficient by itself.

Approval must bind to:
- `request_id`
- approver identity
- current valid TOTP code

### Meaning
This is valid:
- approve request `req_123` with TOTP `482911`

This is **not** valid:
- just present `482911` with no request binding

### Broker verification
Broker must verify:
- request is still pending
- request is not expired
- approver is allowed
- approver is not requester
- TOTP matches that approver
- this approval has not already been consumed

---

## 9. Adapter contract

Adapters are later, but the contract should already be clear.

### Adapter role
Adapter may:
- read pending requests
- render approval messages
- receive human responses
- submit structured decisions to broker

Adapter may **not**:
- write canonical state directly
- execute commands
- bypass broker validation

### Read model
For the first adapter version, adapter may either:
1. read from a read-only projection, or
2. ask broker for a read-only pending-request feed

### Write model
Adapter always writes approval decisions back through the **review socket**, never directly into the state store.

That means:
- adapter reads
- broker writes

This is the key guarantee.

---

## 10. Single-writer rule

`lbd` is the only canonical writer.

That means:
- only `lbd` appends canonical events
- only `lbd` updates projection state
- neither `lb`, `lbctl`, nor adapters mutate state directly

This keeps invariants sane.

---

## 11. Canonical digest contract

Approval is bound to a digest over:
- exact argv
- sanitized execution environment
- cwd
- requester uid
- request id
- expiry

The broker computes and freezes this digest at submit time. Approval binds to that frozen digest, not a value recomputed later.

This prevents:
- approval reuse on another request
- command widening after approval
- silent env manipulation

---

## 12. Sanitized execution contract

Before execve, broker builds the execution context itself.

Minimum requirements:
- fixed PATH
- no requester-controlled preload variables
- controlled cwd
- no inherited requester-controlled file descriptors

This execution context is part of the approved action digest.

---

## 13. Error contract

Every broker response should include:
- `request_id` when available
- `status`
- `message`

Example:

```json
{
  "request_id": "req_123",
  "status": "error",
  "message": "request expired"
}
```

---

## 14. Minimal stable action set

First stable protocol actions:
- `request.submit`
- `request.status`
- `request.execute`
- `review.approve`
- `review.deny`

That is enough to build the first end-to-end seam.
