# Lease Broker Successor — Architecture Outline

_Last updated: 2026-04-20 UTC_

## 1. Goal

Build a **literate permission broker** for Linux.

A requester asks for one privileged action.
A human sees a clear explanation.
The exact action runs once if approved.

---

## 2. Product surface

### Requester UX

Primary form:

```bash
request sudo systemctl restart app-moltpod-backend
```

Optional explicit form:

```bash
lb request -- systemctl restart app-moltpod-backend
```

### Approval UX

Human sees:
- requester
- host
- exact action
- plain-language explanation
- risk/effect summary
- approve / deny

### Result UX

Requester gets:
- request ID
- pending / approved / denied / executed / failed
- final stdout / stderr / exit code if executed

---

## 3. Core runtime model

### Default mode
- non-blocking
- request returns `request_id` + `pending`
- requester polls or asks execute later

### Optional mode
- blocking / interactive
- requester waits for approval/denial and final result

---

## 4. Main components

### A. `request` / `lb` CLI
Purpose:
- submit requests
- check status
- ask broker to execute approved request

Language:
- Go

### B. `lbd` broker daemon
Purpose:
- local trust boundary
- request recording
- approval verification
- one-time exact execution
- audit writing

Language:
- Go

Runs as:
- root

### C. approval view / adapter layer
Purpose:
- display readable approval requests
- collect human decision
- pass structured decision to broker

MVP order:
1. local manual review path
2. TOTP-backed approval confirmation
3. later transport adapters if needed

### D. immutable event log
Purpose:
- canonical history
- append-only truth
- replayable source of state

### E. projection store
Purpose:
- fast lookup of current state
- active requests
- active leases
- execution results

Safe to rebuild from event log.

---

## 5. Communication model

### Between CLI and broker
Use a **Unix domain socket**.

Example path:
- `/run/lb/request.sock`

Why:
- local-only
- simple IPC
- easy systemd fit
- peer credential support

### What the CLI sends
Structured request, roughly:
- action
- argv
- reason
- mode (`poll` or `wait`)

### What broker returns
Structured response, roughly:
- request ID
- status
- message

Example:

```json
{
  "request_id": "req_123",
  "status": "pending",
  "message": "Approval required"
}
```

---

## 6. Request lifecycle

### Step 1 — submit
Requester submits exact argv.

### Step 2 — record
Broker writes append-only event:
- request created

### Step 3 — explain
System generates human-readable description.

Important:
- description may be rewritten for clarity
- executed argv may **not** be rewritten

### Step 4 — review
Human sees request and approves or denies.

### Step 5 — verify approval
Broker verifies:
- request still pending
- request not expired
- approver is authorized
- requester is not approver
- approval is tied to that request ID

### Step 6 — execute once
Broker executes exact approved argv once as root.

### Step 7 — record outcome
Broker writes:
- executed
- failed or succeeded
- exit code
- output metadata

---

## 7. Trust model

### Requester
Can ask.
Cannot approve.
Cannot execute as root.

### Approver
Can approve or deny.
Must be a distinct trusted human identity.

### Broker
Only thing that executes as root.

### Adapter
Carries approval interaction only.
Does not execute privileged actions.

---

## 8. Exact-action binding

Approval must bind to a canonical digest of:
- exact argv
- sanitized execution env
- cwd
- requester uid
- request ID
- expiry

That prevents:
- replay on another request
- widening after approval
- ambiguity about what was approved

---

## 9. TOTP role

TOTP is the first strong approval factor.

Use it to confirm:
- this approver is allowed
- this approver is approving this request

Not as the entire UX.

Meaning:
- human still sees readable approval summary
- TOTP is the confirmation step

---

## 10. Routing policy

This machine may have multiple requesters.
So the broker needs a root-owned routing policy.

It decides:
- which approver set handles requests from which requester
- whether remote approval is allowed
- when to fall back to local manual approval only

Requester never chooses the destination.

---

## 11. Fallbacks

Fallback must exist from day one.

Minimum fallback:
- local manual approval path
- same readable summary
- same exact-action binding
- same audit trail

---

## 12. Storage model

### Canon
Append-only event log.

Properties:
- immutable
- hash-chained
- source of truth

### Projection
Rebuildable indexed state.

Properties:
- current request state
- current lease state
- execution lookup
- disposable and rebuildable

---

## 13. Security defaults

### Execution defaults
Broker executes with:
- sanitized environment
- fixed PATH
- controlled cwd
- no requester-controlled preload tricks
- no inherited requester-controlled file descriptors

### Time defaults
Suggested:
- request TTL: 5 minutes
- one-time nonce: yes
- second execution of same request: forbidden

### File defaults
- routing policy: root-owned
- TOTP secrets: unreadable by requester identities
- audit log: root-readable only

---

## 14. MVP phases

### Phase 1 — protocol and skeleton
- CLI submit/status/execute shape
- broker daemon
- Unix socket contract
- event schema
- projection schema

### Phase 2 — local vertical slice
- submit request
- manual approval
- execute once
- record immutable events
- rebuild projection

### Phase 3 — TOTP-backed approval
- approver identity
- TOTP confirmation bound to request ID
- self-approval blocking

### Phase 4 — polish and fallback hardening
- clearer review UX
- operator docs
- smoke tests
- optional transport adapters later

---

## 15. Suggested code layout

```text
sudo-lease-broker/
  cmd/
    lb/
    lbd/
    lbctl/
  internal/
    protocol/
    socket/
    requests/
    approvals/
    execution/
    events/
    projection/
    policy/
    totp/
    audit/
  docs/
    architecture.md
    protocol.md
    threat-model.md
```

---

## 16. First concrete implementation seam

Start here:

1. `lb request -- <argv...>`
2. send structured request over Unix socket
3. broker returns `request_id` + `pending`
4. `lb status <id>` works
5. `lbctl approve <id>` works
6. `lb execute <id>` runs exact argv once if approved

That is the first real end-to-end seam.

---

## 17. What this is deliberately not

- not a plugin-first product
- not current Airlock reused as-is
- not a general secret vault
- not a shell magic trick
- not a passive interception engine first

---

## 18. Recommendation

Build the smallest credible local loop first:
- request
- explain
- approve
- execute once
- record immutably

Everything else comes after that works cleanly.
