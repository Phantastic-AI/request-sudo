# request-sudo

Literate permission broker for Linux: standalone CLI + root-owned daemon. Replaces the OpenClaw lease broker plugin (LB2) in role, but uses a one-request-one-approval-one-execution model rather than reusable lease semantics. See `CONTEXT.md` and `docs/adr/0001-drop-lease-terminology.md`.

See `README.md` for the product purpose, `SPEC.md` for product/design inputs, `PROTOCOL.md` for the broker contract, and `STATE_MACHINE.md` for the request lifecycle.

## Agent skills

### Issue tracker

Issues for this repo live as **Hub tasks under project `RSUDO`**. Use the `hub` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical defaults — `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
