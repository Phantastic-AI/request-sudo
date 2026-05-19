# request-sudo

A literate permission broker for Linux. A user or agent asks for one privileged action; a human sees a clear explanation; the exact action runs once if approved.

## Language

**request-sudo**:
The product. The CLI a user or agent runs to ask for a privileged action.
_Avoid_: lease broker, broker (those refer to other things below).

**broker**:
The trusted root-owned daemon (`request-sudod`) that records requests, validates approvals, and executes approved actions. Single writer, single executor.
_Avoid_: server, daemon (too generic), lease broker.

**Request**:
A submitted privileged action: exact argv plus reason and mode. Has an `id`, lifecycle state, and a frozen approval digest.
_Avoid_: lease, command, action (action is reserved for the future action broker — see Out of scope).

**Approval**:
A one-time consumable execution grant bound to one Request via the canonical digest.
_Avoid_: lease, grant (used informally; "Approval" is the canonical term), permission.

**Approver**:
A trusted human or service identity allowed to approve or deny on the review socket. Cannot be the Requester of the same Request.

**Approver set**:
A named group of approver identities (e.g. `operators`) defined in the broker's static policy file. Each Requester is mapped to one Approver set; the broker accepts approval for a Request only from identities in that set. See [ADR-0002](docs/adr/0002-static-file-approver-routing.md).

**Requester**:
The local UID that submitted the Request. Identified at the requester socket via SO_PEERCRED.

**Canonical digest**:
A hash bound to a Request at submit time over: exact argv, sanitized environment, cwd, requester uid, request id, expiry. Approval binds to this frozen digest, not a value recomputed at execution. Prevents approval reuse, command widening, and silent env manipulation.
_Avoid_: signature, hash, fingerprint.

**Sanitized execution context**:
The deterministic execution environment the broker constructs before execve: fixed PATH, env allowlist, fixed cwd, no inherited fds. Part of the canonical digest. See [ADR-0003](docs/adr/0003-sanitized-execution-context.md) for exact values.

**Single-writer rule**:
Only `request-sudod` appends canonical events or mutates projection state. `request-sudo`, `request-sudoctl`, and any future adapters can only request changes via sockets — they never write canonical state directly.

**Canon**:
The append-only hash-chained event log. Source of truth.

**Projection**:
The rebuildable current-state view derived from canon. Safe to discard and rebuild.

**Requester socket**:
`/run/request-sudo/request.sock`. Receives `request.submit`, `request.status`, `request.execute` from low-privilege callers.

**Review socket**:
`/run/request-sudo/review.sock`. Receives `review.approve`, `review.deny` from trusted approval tooling. Must never be treated as equivalent to the requester socket.

## Relationships

- A **Requester** submits a **Request** on the requester socket.
- The **broker** computes a **Canonical digest** at submit time and freezes it on the **Request**.
- An **Approver** approves or denies the **Request** on the review socket.
- An **Approval** is bound to exactly one **Request** via the **Canonical digest**.
- Each **Requester** maps to one **Approver set** in the broker's policy file; only identities in that set can approve **Request**s from that **Requester**.
- The **broker** consumes the **Approval** at execve start and emits events on the **Canon**.
- The **Projection** is rebuilt from the **Canon** on broker startup and on demand.

## Out of scope (deliberately)

**lease**: The model is one-request-one-approval-one-execution. Approvals are not reusable, not time-extensible, not partially redeemable. The OpenClaw plugin in `../openclaw-lease-broker` was conceptually a lease broker; **request-sudo is not its successor in semantics — only in role**. See [ADR-0001](docs/adr/0001-drop-lease-terminology.md).

**Action broker**: Cross-machine, RPC-style privileged actions (e.g., "pair gateway with user's PC", "run X on host Y") are a *different* broker, not request-sudo. They share design DNA (root-owned daemon, append-only log, exact-binding approvals, single-writer rule) but the contract is different and they will live in a separate package. Until that broker exists, those use cases are unaddressed by this repo. See [ADR-0001](docs/adr/0001-drop-lease-terminology.md).

## Example dialogue

> **Dev:** "If the same approver approves the same `systemctl restart` argv from two different requesters, do we share the **Approval**?"
> **Domain expert:** "No — each **Request** has its own **Canonical digest** that includes the requester uid and request id. The approvals are different objects bound to different requests, even if the argv is identical. That's the whole point of the digest binding — you can't reuse approval capital across requests."

> **Dev:** "Can a **Requester** also be the **Approver** for their own **Request**?"
> **Domain expert:** "No. The broker rejects approval if `approver.id == request.requester.uid`. That's a hard invariant, not a policy knob."

## Flagged ambiguities

- **"lease"** was the codename for the older OpenClaw plugin and was used informally to describe approval grants in early request-sudo docs. Resolved 2026-05-01: deprecated. Use **Request** + **Approval**. See ADR-0001.
- **"action"** is informally used in protocol field names (e.g. `"action": "request.submit"` in JSON). That usage is a JSON-level message-type discriminator, not the domain concept. The future cross-machine broker will own the domain word "action"; in this repo, prefer **Request** when discussing the domain object.
