# request-sudo

This is the clean successor workspace.

request-sudo is the product-facing name.
The broker remains the internal architectural role name: the trusted root-owned component that records requests, verifies approvals, and executes approved actions.

Purpose:
- design and build the next-generation lease broker as a standalone Linux CLI + local root-owned daemon
- keep it separate from the current OpenClaw plugin implementation in `../openclaw-lease-broker`
- treat `../openclaw-lease-broker-ops` as the legacy/current deployment-specific sibling once its remote is recovered or rebuilt

Current source inputs:
- current broker code: `../openclaw-lease-broker`
- discovery notes: `../openclaw-lease-broker-ops/DISCOVERY.md`

Phase 1 working docs:
- frozen product/design inputs: `SPEC.md`, `PLAN.md`, `ARCHITECTURE_OUTLINE.md`, `PROTOCOL.md`, `STATE_MACHINE.md`
- review guide: `docs/PHASE1_REVIEW.md`
- smoke path: `docs/SMOKE_PATH.md`
- verification scaffold: `docs/VERIFICATION.md`
- current review findings: `docs/REVIEW_FINDINGS.md`

Non-goals for v1 successor:
- do not depend on current Airlock `/ssh/sign`
- do not rely on passive filesystem hooks as the primary trigger path
- do not copy runnable artifacts from discovery-one evidence
