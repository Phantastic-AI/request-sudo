# Phase 1 smoke path

_Last updated: 2026-04-20 UTC_

This is the minimal end-to-end path the successor should satisfy before phase 1 is called working.
It intentionally uses only the frozen local manual-review model.

A machine-readable version of this path also lives at `tests/contracts/testdata/smoke/local_manual_approval.json`.

## Preconditions

- `request-sudod` is running as root or under an equivalent local test harness
- request socket exists at `/run/request-sudo/request.sock`
- review socket exists at `/run/request-sudo/review.sock`
- a writable append-only event log path is configured
- projection rebuild uses only the event log as source of truth

## Scenario

Requester asks to restart the backend after deploy.
Approver reviews locally.
Broker executes exactly once.
Requester reads the result.

## Step 1: submit request

Conceptual CLI:

```bash
request-sudo request -- systemctl restart app-moltpod-backend
```

Expected request payload on `/run/request-sudo/request.sock`:

```json
{
  "action": "request.submit",
  "argv": ["systemctl", "restart", "app-moltpod-backend"],
  "reason": "restart after deploy",
  "mode": "poll"
}
```

Expected response:

```json
{
  "request_id": "req_<opaque>",
  "status": "pending",
  "message": "Approval required"
}
```

Expected durable effect:

- append `request_created`
- projection shows request in `pending`
- stored digest binds `argv`, sanitized env, cwd, requester uid, request id, and expiry

## Step 2: review request

Conceptual local review tool action on `/run/request-sudo/review.sock`:

- fetch pending request details
- show requester, host, exact action, human-readable explanation, risk/effect summary
- choose approve or deny

Approval input must stay bound to the request ID under review.

Expected durable effect for approve:

- append `request_approved`
- projection shows request in `approved`
- approver identity is recorded separately from the requester identity

## Step 3: execute approved request

Conceptual CLI:

```bash
request-sudo execute req_<opaque>
```

Expected request payload on `/run/request-sudo/request.sock`:

```json
{
  "action": "request.execute",
  "request_id": "req_<opaque>"
}
```

Expected durable effects:

1. append `execution_started`
2. projection moves `approved -> executing`
3. broker performs one `execve` for the exact approved `argv`
4. append `execution_succeeded` or `execution_failed`
5. projection lands in `executed` or `failed`

## Step 4: read final status

Conceptual CLI:

```bash
request-sudo status req_<opaque>
```

Expected final response shape:

```json
{
  "request_id": "req_<opaque>",
  "status": "executed",
  "exit_code": 0,
  "stdout": "",
  "stderr": ""
}
```

## Replay checks

The same request must not launch twice.

Required smoke assertions:

- a second `request-sudo execute req_<opaque>` does not run another command
- a denied request never reaches `executing`
- an expired request never reaches `approved`
- recovery from an `executing` tail marks the request failed without re-running it

## Minimum audit sequence

Successful path:

1. `request_created`
2. `request_approved`
3. `execution_started`
4. `execution_succeeded`

Failure path after launch:

1. `request_created`
2. `request_approved`
3. `execution_started`
4. `execution_failed`

Recovery path after crash during execution:

1. `request_created`
2. `request_approved`
3. `execution_started`
4. `recovery_marked_failed`

## What is out of scope for this smoke path

- remote approval transport
- plugin-mediated request capture
- SMS or phone-specific approval UX
- multi-step privileged sessions
- reusable privilege leases beyond the single request execution grant
