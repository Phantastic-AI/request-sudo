---
status: accepted
date: 2026-05-01
---

# Static-file approver routing for first slice

The broker needs a policy lookup on every approval: "is approver A allowed to approve a Request from requester R?". For the first slice, this policy lives in a single root-owned YAML file at `/etc/request-sudo/approvers.yaml` loaded at broker startup (and on SIGHUP). It defines named approver sets and maps each known requester identity to one set.

## Format

```yaml
approver_sets:
  operators:
    - root
    - aditya
    - dominic
routing:
  openclaw:
    approver_set: operators
    capture_output: plain          # see ADR-0004
    max_execution_seconds: 600     # see ADR-0004
  aditya:
    approver_set: operators
    capture_output: interactive
  leastpriv:
    approver_set: operators
# requesters not listed here fall back to local-only manual approval (see Local-only fallback below)
```

Each routing entry is an object. The simple-string form `openclaw: operators` from earlier drafts is rejected by the v1 parser. Required field per entry: `approver_set`. Optional fields: `capture_output` (default `interactive`, see ADR-0004), `max_execution_seconds` (default 300, hard cap 3600, see ADR-0004).

The broker loads this file at startup; missing, unparseable, or schema-invalid is a hard startup failure (fail loudly, not silently). SIGHUP reloads.

### Local-only fallback

When a requester is not present in `routing`, the broker accepts the request and creates the corresponding canon event, but only allows approval from the **review socket** by **a UID that is a member of any defined `approver_set`**. There is no remote route, no policy mapping, no auto-approval — a local operator must explicitly approve via `request-sudoctl approve <id>` while logged in on the same machine. This is the "no remote route" path from `SPEC.md`.

### Pending-request policy resolution

Policy is **re-resolved at approve time**, not at submit time. If the policy file is reloaded (SIGHUP) between submit and approve — for example, an approver was removed because their account was compromised — the approval check uses the *current* policy. Pending requests that were submitted under an old policy do not carry an approver-set snapshot; revocation is immediate on next reload. This means:
- Adding an approver to a set: pending requests can now be approved by them
- Removing an approver from a set: their previously-allowed pending requests cannot be approved by them anymore (broker rejects with `auth: not in approver_set`)
- Changing the approver_set for a requester: pending requests now route to the new set; if the new set is empty or undefined, all pending become unapprovable until policy is fixed

## Test plan (must all be red before any implementation)

- **T1.** Valid policy file at `/etc/request-sudo/approvers.yaml` parses and resolves: routing for known requester returns the correct approver_set; unknown requester falls back to local-only.
- **T2.** Missing policy file at startup: broker refuses to start with `policy: file missing`. Does not silently default-allow anything.
- **T3.** Unparseable YAML: broker refuses to start with a parse error including line number.
- **T4.** Routing entry references a non-existent approver_set: broker refuses to start with `policy: routing references undefined approver_set`.
- **T5.** Empty `approver_sets` block: broker starts but every approval is rejected (no one is in any set). Documented behavior, not a startup failure.
- **T6.** Approval attempt: approver in the matching set succeeds; approver not in the set is rejected with `auth: not in approver_set for this requester`.
- **T7.** Approver-equals-requester rule: even if the approver UID is in the routing's approver_set, approval from that UID is rejected with `auth: approver cannot be the requester` (this is in PROTOCOL.md but tested here for the policy-loaded path).
- **T8.** SIGHUP after editing the policy file: broker reloads atomically. Pending requests submitted before the reload re-resolve against the *new* policy at approve time (revocation is immediate, not snapshot-bound). Removing a UID from `approver_set` causes their pending approvals to be rejected on the next call.
- **T9.** SIGHUP with broken policy file: broker keeps running with the previous valid policy and logs a reload error; does not crash.
- **T10.** Default installer-shipped policy (operators=[root], no routing entries): every requester falls back to local-only manual approval; no remote approval is possible.
- **T11.** Policy file `/etc/request-sudo/approvers.yaml` ownership: not `root:root` → broker refuses to start with `policy: file ownership must be root:root`.
- **T12.** Policy file mode: any group/other write bit set → broker refuses to start with `policy: file must be mode 0640 or stricter`.
- **T13.** Policy file is a symlink: broker opens with `O_NOFOLLOW`; any symlink at the policy path causes startup failure.
- **T14.** Policy file's parent directory `/etc/request-sudo/` is group/other writable: broker refuses to start with `policy: parent directory writable by non-root`.
- **T15.** Routing entry uses the rejected simple-string form (`openclaw: operators` instead of `openclaw: { approver_set: operators }`): broker refuses to start with a parse error pointing at the offending line.
- **T16.** Local-only fallback: a request from an unrouted requester arrives. Approval from a UID in *any* defined `approver_set` (delivered on the review socket) succeeds; approval from a UID not in any set is rejected.

## Considered options

- **Unix-group-based routing** (rejected): Couples security policy to mutable group membership. `usermod -aG` from any privileged context would silently change who can approve what. Harder to audit. Approver routing is a *security* policy and deserves explicit enumeration.
- **Per-requester drop-in directory** at `/etc/request-sudo/policy.d/` (rejected for now): Useful when there are 5+ distinct requesters or per-requester options like default TTL or argv globs. Premature for the first slice. Migration path is straightforward — write one drop-in file per requester and have the broker merge them.

## Consequences

- The installer ships a default `/etc/request-sudo/approvers.yaml` with `operators: [root]` and no `routing` entries, so Phase 3 ships safe-by-default (everything falls back to local manual approval until an admin maps requesters).
- A `request-sudoctl policy show` subcommand should print the loaded, resolved policy so admins can verify what's in effect (out of scope for this ADR but tracked as follow-up).
- When the action broker repo lands, it will have its own routing policy. The two are not shared.
