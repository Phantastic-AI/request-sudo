# request-sudo — State Machine

_Last updated: 2026-04-20 UTC_

## 1. Request states

Canonical request states:
- `pending`
- `approved`
- `denied`
- `expired`
- `executing`
- `executed`
- `failed`
- `revoked`

---

## 2. Lifecycle

### Submit
`pending`

### Approve
`pending -> approved`

### Deny
`pending -> denied`

### Expire before approval
`pending -> expired`

### Start execution
`approved -> executing`

Execution should consume the approval at start, not at the end.

### Execute success
`executing -> executed`

### Execute failure
`executing -> failed`

### Revoke
Possible from:
- `approved -> revoked`
- possibly `pending -> revoked` if operator cancels it explicitly

---

## 3. Forbidden transitions

These should be rejected:
- `denied -> approved`
- `executed -> approved`
- `executed -> executing`
- `expired -> approved`
- `failed -> executing`
- `revoked -> executing`
- `executing -> revoked`

If a new attempt is needed, it should create a **new request**.

---

## 4. Why `executing` exists

It closes the replay hole.

Without `executing`, a broker crash during launch makes state ambiguous.

With `executing`:
- approval is consumed
- crash recovery can treat request as terminally in-flight or failed
- no second execution should be allowed on the same request ID

---

## 5. Lease model

For MVP, treat approval as a one-time consumable execution grant attached to the request.

Externally and internally, the important truth is:
- one request
- one approval
- one execution attempt

---

## 6. Time rules

Suggested defaults:
- request TTL: 5 minutes
- approval nonce: one-time
- request cannot execute twice

---

## 7. Audit events

Minimum event types:
- `request_created`
- `request_approved`
- `request_denied`
- `request_expired`
- `request_revoked`
- `execution_started`
- `execution_succeeded`
- `execution_failed`

Each event should include:
- event id
- previous event hash
- request id
- timestamp
- actor
- event type
- summary details

---

## 8. Recovery rule

On restart, broker rebuilds projection from canonical log.

If last state is `executing` with no terminal event:
- do **not** re-run automatically
- mark it `failed` during recovery
- write a `recovery_marked_failed` event
- never silently replay execution

Operator review may still happen later, but recovery itself should deterministically mark the request failed.
