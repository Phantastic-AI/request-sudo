---
status: accepted
date: 2026-05-01
---

# Drop "lease" terminology and scope request-sudo to argv-based local privileged execution

The older OpenClaw plugin (`../openclaw-lease-broker`) was conceptually a lease broker — delegated, possibly reusable privilege capital. request-sudo intentionally rejects that model: every Request has exactly one Approval bound by canonical digest and exactly one execution attempt. Continuing to call this a "lease broker" misleads readers about the semantics.

We are also explicitly **not** trying to build the cross-machine privileged-action broker (e.g. "pair gateway with user's PC", "perform action on host Y") in this repo. That is a separate broker with a different contract; it can share design DNA (root-owned daemon, append-only event log, exact-binding approvals, single-writer rule) but is out of scope here.

## Decision

1. The active vocabulary is **Request** + **Approval** + **broker** (internal role) + **request-sudo** (product). The word **lease** is deprecated; only allowed when explicitly referencing the legacy plugin path.
2. request-sudo's contract is argv-based local privileged execution. argv-out-of-scope cases (RPC-style actions, cross-machine actions, structured action params) are deferred to a separate, future broker.

## Test plan

- **T1.** `grep -rn "lease broker" /srv/moltpod-src/security/request-sudo/` returns matches only in: ADR-0001/CONTEXT.md (the deprecation notes themselves), README.md/AGENTS.md/PHASE1_REVIEW.md (historical-pointer paths to `../openclaw-lease-broker*`), and SMOKE_PATH.md (out-of-scope marker). No new doc may introduce "lease broker" as the active project description.
- **T2.** `grep -rn "Lease " STATE_MACHINE.md` returns zero matches (the heading is "Approval consumption model").
- **T3.** PROTOCOL.md does not contain `target_machine`, `host`, `rpc`, or any field hinting at cross-machine action params; argv is the only execution surface.
- **T4.** Code `grep` of `internal/` and `cmd/` returns zero `Lease` types or `lease` identifiers (case-insensitive).

## Considered options

- **Keep "lease broker" as the project description for continuity with the old codebase.** Rejected: the semantic model is opposite (one-shot, not reusable). Continuity-of-name without continuity-of-meaning is a footgun.
- **Generalize request-sudo to handle both argv local privilege and RPC cross-machine actions.** Rejected: bloats the schema and digest path; weakens audit clarity; makes the package un-releasable as a focused tool.
- **Coin a new word like "permit" or "voucher".** Rejected: code already uses `Request` and `ApprovalSummary`; introducing a third term creates more drift, not less.

## Consequences

- Docs that still describe the project as "lease broker successor" must be updated to "permission broker" / "literate sudo broker" or similar. Historical pointers to the old plugin path remain.
- `STATE_MACHINE.md §5` was renamed from "Lease model" to "Approval consumption model" as part of this ADR; the body now also explicitly references this ADR.
- Any future cross-machine privileged-action work needs its own repo, package name, and ADR-0001 in *that* repo.
- Releasable scope: request-sudo can be open-sourced as a focused literate sudo replacement.
