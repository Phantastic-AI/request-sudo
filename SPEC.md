# request-sudo

_Last updated: 2026-04-19 UTC_

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

Not a weird operator ritual.
Not a plugin-first architecture.
Not a bag of hidden magic.

---

## What it is

The successor is a **literate permission broker** for Linux.

It exists to let a normally unprivileged user or agent ask for one privileged action, get a clean human approval flow, and then run exactly that action once.

The approval should feel readable.
The execution should feel safe.
The audit trail should be real.

---

## What it is not

It is **not** centered on:
- OpenClaw plugin hooks
- passive filesystem interception
- current Airlock `/ssh/sign`
- making humans type ugly low-level command syntax

Those may become integrations later.
They are not the center of this design.

---

## The core idea

There are two layers:

### 1. Human-facing layer
This is what the human sees.

It should answer:
- who is asking?
- what do they want to do?
- why are they asking?
- what is the risk?
- approve or deny?

This should be written in plain language.

### 2. Broker layer
This is the trusted machinery underneath.

It should:
- bind approval to the exact action
- enforce one-time execution
- enforce expiry
- prevent replay
- keep an immutable history
- be the only component allowed to execute as root

The human should mostly not have to think about this layer.

---

## Desired user experience

### Request side
A requester says:

```bash
request-sudo systemctl restart app-moltpod-backend
```

The system keeps the exact submitted argv as the thing that may run.
It separately creates a human-readable explanation such as:

> `openclaw` wants to restart `app-moltpod-backend` on `discovery-two`.
>
> Why: restart backend after deploy.
>
> Effect: the backend process will restart.
>
> Risk: brief interruption if restart fails.

### What the requester gets back

By default, the system should be **non-blocking**.

The request command returns a request handle, not privilege.

Example:

```json
{
  "request_id": "req_123",
  "status": "pending",
  "message": "Approval required"
}
```

This means:
- the request was recorded
- no privileged action has run yet
- the requester does not have sudo or root access

### Default mode: polling

The normal model should be:
1. requester submits request
2. system returns `request_id` + `pending`
3. requester polls status later
4. if approved, requester asks broker to execute that specific request
5. broker runs it once and returns the result

Example shape:

```bash
request-sudo systemctl restart app-moltpod-backend
# -> req_123 pending

request status req_123
# -> approved

request execute req_123
# -> stdout/stderr/exit code
```

This is the best default for agents and background flows.

### Optional mode: interactive / wait

There should also be an optional blocking mode for humans or manual shells.

Example:

```bash
request-sudo systemctl restart app-moltpod-backend --wait
```

That mode may:
- wait for approval or denial
- then return the final result directly

But this is a convenience mode, not the core model.

### Approval side
The approver should see a clean approval prompt and be able to say yes or no simply.

Approval is bound to a canonical digest of:
- argv
- sanitized execution environment
- cwd
- requester uid
- request id
- expiry

Examples of acceptable UX later:
- a local review screen
- a tiny terminal review prompt
- a text message
- a message adapter

But the wording should stay literate and clear.

### Execution side
If approved:
- the broker runs the exact action once
- the requester sees result output
- the event is recorded

If denied:
- the requester sees that clearly
- nothing runs

### Important implementation truth
The requester never receives general privileged capability.

It gets:
- a request id
- a status
- later, possibly, a result

It does not get:
- root
- raw sudo access
- a reusable broad privilege token

---

## Approval model

Approval should be easy for the human, but strict underneath.

### Good approval model
- human sees a readable summary
- human confirms with a simple action
- broker binds approval to the exact request

### Bad approval model
- approval is ambiguous
- approval can be replayed onto a different request
- approval can be faked by the requester runtime

---

## TOTP

TOTP is a good fit for the first strong approval mechanism.

Why:
- no carrier dependency
- no delivery lag
- no phone-routing bottleneck as the main design
- multiple trusted approvers can each have their own secret
- easy to keep local and explicit

### Important rule
TOTP should **support** approval, not replace the readable approval step.

TOTP alone is not approval. Approval must include the request id being approved.

Meaning:
- the human still sees what they are approving
- TOTP simply confirms that the human approving is actually authorized

So the experience should feel like:
- readable approval prompt
- simple approve action
- TOTP confirmation behind that

Under the hood, the approver confirms a specific request id, not just a floating code.

Not like a crypto puzzle.

---

## Multi-user routing

This machine may have multiple requesters.

That means the system needs a routing policy.

Question:
**when user X asks for sudo, which approver or approver set should handle it?**

The answer should come from policy, not from the requester.

### Example
- `openclaw` requests route to the designated operator approver set
- `leastpriv` test user routes to the same or a broader operator set
- unknown users may have no remote route and require local manual approval only

### Principle
The requester must never choose where approval is sent.

---

## Fallbacks

A good approval system needs fallbacks.

Because sometimes:
- you do not have SMS
- you do not have the right device nearby
- a transport is down
- you just want to approve locally and move on

### So the fallback should be:
- local manual review path
- same clear approval summary
- same exact-command binding
- same audit trail

This should be part of the design from the beginning.

---

## Trust boundary

This is the most important technical rule:

**only the broker executes as root**

Everything else:
- requester
- adapter
- review UI
- helper scripts

should stay outside that trust boundary.

---

## Storage model

We want two things:

1. immutable history
2. fast current-state lookup

So the right model is:

### Canonical truth
An append-only event log.

This is the real history.
It should not be mutated.

The log should be hash-chained so tampering is detectable. Optional signing can be added on top.

### Derived state
A rebuildable projection.

This exists only to answer “what is true right now?” quickly.

That lets us keep both:
- immutability
- operability

---

## FBP / flow-based-programming spec

The FBP spec is useful here **as a modeling layer**.

It is a good fit for:
- describing approval workflows
- describing adapters
- describing policy flows
- making the system visual later

It is **not** the core runtime for the MVP.

So:
- yes for workflow description
- no for replacing the broker core

---

## Repo strategy

Start fresh.

Use the old broker repo as reference only.

### Current reference repo
- `/srv/moltpod/security/openclaw-lease-broker`

### Current successor workspace
- `/srv/moltpod/security/request-sudo`

### Recommended fresh repos
- core repo: `sudo-lease-broker`
- ops repo: `sudo-lease-broker-ops`

---

## MVP

A winning MVP should do only a few things, but do them well.

### MVP should include
- a clean request command
- a root-owned broker
- exact one-time execution
- readable approval summary
- TOTP-backed approval confirmation
- local manual fallback
- immutable event log
- rebuildable current-state projection
- clear audit trail
- sanitized execution environment
- concrete TTL and replay rules

### MVP should not include
- plugin-first logic
- remote fleet execution
- current Airlock dependency
- passive interception as the main path
- complicated approval transports before the core is solid

---

## MVP flow

1. requester asks for one privileged action
2. broker records request
3. broker presents a readable approval summary
4. approver confirms with a simple approval step plus TOTP
5. broker executes exact action once
6. broker records result
7. requester sees outcome

---

## Roles

Keep them separate.

### Requester
May ask for privilege.
May not approve.
May not execute as root.
May not read or control approver TOTP secrets.

### Approver
May approve or deny.
Must be authenticated as an allowed human.
Should not be silently conflated with a bot runtime.
Must be a distinct identity from the requester.

### Broker
Runs as root.
Only component allowed to execute the approved privileged action.

### Adapter / transport
Only carries approval interaction.
Does not execute privileged actions.
Does not hold requester-controlled secrets.

---

## What success looks like

A least-privilege test user can request:

```bash
request-sudo systemctl restart app-moltpod-backend
```

A human sees a clean explanation.
The human approves simply.
TOTP confirms identity.
The exact action runs once.
The result is shown.
The whole thing is recorded.

That is the winning first version.

---

## Recommendation

Keep the design simple and product-first.

Build the successor as:
- a literate permission broker
- with a readable approval experience
- backed by exact one-time execution
- with TOTP as the first strong approval factor
- and a manual local fallback from day one


## Minimal execution safety

Before execve, the broker should build a sanitized execution environment:
- fixed PATH
- no requester-controlled preload variables
- controlled cwd
- no inherited requester-controlled file descriptors

## Minimal audit fields

Each audit event should at least contain:
- event id
- previous event hash
- timestamp
- request id
- event type
- actor identity
- summary details
