# request-sudo

_Last updated: 2026-04-20 UTC_

## What this should feel like

This should feel simple.

A user or agent says something like:

- `request-sudo restart the backend`
- `request-sudo install jq`
- `request-sudo add this host entry`

The system then:
1. turns that request into a clear human-readable explanation
2. shows the human exactly what would happen
3. asks for approval in a simple way
4. runs the exact command once if approved
5. records what happened

Important: the broker executes the requester's exact submitted argv verbatim. The rewrite is only the human-readable description, never the executed command.

That is the product.

---

## What it is

request-sudo is a **literate permission broker** for Linux.

The product-facing name is `request-sudo`.
The internal word `broker` is intentional: it names the trusted root-owned component inside the system, not an old leftover brand.

It exists to let a normally unprivileged user or agent ask for one privileged action, get a clean human approval flow, and then run exactly that action once.

---

## What it is not

It is **not** centered on:
- OpenClaw plugin hooks
- passive filesystem interception
- current Airlock `/ssh/sign`
- making humans type ugly low-level command syntax

---

## Desired user experience

### Request side
A requester says:

```bash
request-sudo request -- systemctl restart app-moltpod-backend
```

The system keeps the exact submitted argv as the thing that may run.
It separately creates a human-readable explanation such as:

> `openclaw` wants to restart `app-moltpod-backend` on `discovery-two`.\n>
> Why: restart backend after deploy.\n>
> Effect: the backend process will restart.\n>
> Risk: brief interruption if restart fails.

### What the requester gets back

By default, the system is **non-blocking**.

Example:

```json
{
  "request_id": "req_123",
  "status": "pending",
  "message": "Approval required"
}
```

This means the requester has a handle, not privilege.

### Default mode: polling

The normal model is:
1. requester submits request
2. system returns `request_id` + `pending`
3. requester polls status later
4. if approved, requester asks broker to execute that specific request
5. broker runs it once and returns the result

### Optional mode: interactive / wait

There can also be an optional blocking mode for humans or manual shells.

---

## Approval model

Approval should be easy for the human, but strict underneath.

### Good approval model
- human sees a readable summary
- human confirms with a simple action
- broker binds approval to the exact request

### Future SMS/chat path
For the SMS/chat path, the transport can carry a short approval token such as `A7K`.

That token is:
- case-insensitive
- tied to one request
- not valid by itself without request binding

The human-facing reply style can be simple, for example:
- `yes A7K`
- `no A7K`

That transport is future work. The current runtime slice is local manual approval first.

---

## Multi-user routing

This machine may have multiple requesters.

The answer to “who gets the approval request?” must come from root-owned policy, not from the requester.

Examples:
- `openclaw` → operator approver set
- `leastpriv` → operator approver set
- unknown users → no remote route, manual local approval only

---

## Fallbacks

A good approval system needs fallbacks.

Minimum fallback:
- local manual review path
- same clear approval summary
- same exact-command binding
- same audit trail

---

## Storage model

### Canon
Append-only event log.
Hash-chained.
Source of truth.

### Projection
Rebuildable current-state view.
Safe to discard and rebuild.

---

## FBP usage

The flow-based-programming spec is useful as a workflow description layer, not the broker runtime.

---

## Recommendation

Build the first version around clarity:
- natural request
- readable approval
- exact one-time execution
- immutable history
- local fallback
