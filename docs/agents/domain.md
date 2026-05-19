# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root.
- **`docs/adr/`** — read ADRs that touch the area you're about to work in.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The producer skill (`/grill-with-docs`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo (this is one):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-...md
│   └── 0002-...md
├── cmd/
├── internal/
├── tests/
└── ...
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

For request-sudo specifically, terminology to be careful with:
- **request-sudo** — the product-facing name (use externally, in user-visible output, in issue titles).
- **broker** — the internal architectural role (use in code, ADRs, design docs).
- **lease**, **request**, **approval** — defined in `PROTOCOL.md` and `STATE_MACHINE.md`. Don't reinvent these.

If the concept you need isn't in the glossary yet, that's a signal — either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/grill-with-docs`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders) — but worth reopening because…_
