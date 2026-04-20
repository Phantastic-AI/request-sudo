# request-sudo — MVP Plan

_Last updated: 2026-04-19 UTC_

## The decision
Start fresh.

Do not turn the existing plugin repo into the successor.
Use it as reference only.

The successor should be a **literate permission broker** with:
- simple request UX
- readable approval UX
- exact one-time privileged execution
- TOTP-backed approval confirmation bound to a specific request id
- local manual fallback

---

## Why this is the right MVP

Because it keeps the product understandable.

We want the human experience to be:
- someone asks for one privileged action
- the system describes it clearly
- the human approves or denies simply
- the system runs exactly that action once

Important: the executed argv is the requester's exact submitted argv. The system may rewrite only the human-readable description, never the command that runs.

That is the product.

---

## Principles

1. **Readable first**
2. **Exact execution**
3. **Immutable history**
4. **Only broker runs as root**
5. **Fallbacks from day one**
6. **OpenClaw optional**

---

## The shape of the MVP

### User-facing request
Example:

```bash
request-sudo systemctl restart app-moltpod-backend
```

### Human-facing approval
The approver should see:
- who is asking
- what they want to do
- why
- likely effect
- risk
- approve / deny

Approval must bind to a request id plus the exact execution digest, not just a generic approval gesture.

### Trust confirmation
TOTP confirms the approver identity.

### Execution
The broker runs the exact action once.

### Audit
Everything is recorded in immutable history.

---

## Roles and permissions

### Requester
- can request
- cannot approve
- cannot execute root directly
- cannot read or control approver TOTP secrets

### Approver
- can approve or deny if policy allows
- must authenticate as a trusted human
- should not be confused with the requester runtime
- must be distinct from the requester

### Broker
- runs as root
- sole privileged executor

### Adapter / transport
- carries approval interaction only
- does not execute privileged actions

This is how we avoid user and permission clashes.

---

## Routing policy

Different local users may trigger requests on this machine.
So the system needs a simple routing policy.

### Rule
The requester does **not** choose the destination approver.

### Policy decides
Examples:
- `openclaw` → operator approver set
- `leastpriv` → operator approver set
- unknown users → no remote route, manual local fallback only

This keeps routing deterministic and safe.

---

## TOTP in plain English

TOTP is the first strong approval factor.

Why we want it:
- no SMS dependency as the core path
- no phone bottleneck as the main design
- multiple trusted approvers can exist
- clean local security story

### Important nuance
TOTP is **not** the product experience.
It is the confirmation step behind the approval.

It must be tied to a specific request id so one TOTP code cannot silently approve the wrong pending request.

So the user should feel:
- readable approval prompt
- simple approve / deny step
- TOTP confirms that the approver is real

---

## Fallbacks

We need fallbacks because the human cannot always use the same transport.

### MVP fallback
Local manual approval path.

That means:
- same readable request summary
- same exact-action binding
- same audit trail
- no dependency on SMS or external transport

---

## Storage model

### Canon
Append-only event log.
This is the source of truth.
Use hash-chained entries so tampering is detectable.

### Projection
Rebuildable current-state view.
This exists for fast lookup only.

This gives us:
- immutability
- queryability
- recoverability

---

## FBP usage

Use the flow-based-programming spec as a **workflow description layer**, not the broker runtime.

Good for:
- approval flow diagrams
- adapter flow design
- future visual tooling

Not for:
- replacing the daemon core
- replacing the event log
- replacing the exact execution path

---

## MVP phases

### Phase 1 — shape the core
- define request shape
- define approval summary shape
- define immutable event format
- define projection model
- define role separation clearly

### Phase 2 — local working slice
- request command works
- broker records request
- local manual approval works
- broker executes exact action once
- immutable log is written
- projection rebuild works

### Phase 3 — TOTP-backed approval
- approver identity model is added
- TOTP confirmation is added and tied to a specific request id
- self-approval is blocked by identity separation and secret placement
- audit trail records the approval factor cleanly

### Phase 4 — first remote convenience transport
- only after the core is solid
- transport should remain outside execution trust boundary

---

## Verification

The MVP is good when we can prove this:

1. least-privilege user requests one privileged action
2. request is rewritten into a readable approval summary
3. approver sees that summary
4. approver confirms with TOTP
5. broker executes the exact action once
6. replay fails
7. denial prevents execution
8. local fallback works if remote transport is unavailable
9. immutable log reconstructs final state correctly

---

## What we are deliberately not doing yet

- not plugin-first
- not Airlock-first
- not weird operator incantations as the product surface
- not remote fleet execution
- not passive interception as the core UX

---

## Recommendation

Build the first version around clarity.

That means:
- natural request
- readable approval
- TOTP-backed human confirmation
- exact one-time execution
- immutable history
- local fallback

That is the winning MVP.


## Concrete defaults

- request TTL: 5 minutes
- one-time approval nonce: yes
- second execution of same request id: forbidden
- routing policy file: root-owned
- audit log permissions: root-readable only
