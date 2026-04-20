# Phase 1 verification scaffold

_Last updated: 2026-04-20 UTC_

This file defines the default verification sequence for the successor build.
Use the smallest built-in toolchain that proves correctness.

## Documentation checks

Run these immediately when design-adjacent docs change:

```bash
test -f SPEC.md
test -f PLAN.md
test -f ARCHITECTURE_OUTLINE.md
test -f PROTOCOL.md
test -f STATE_MACHINE.md
test -f docs/PHASE1_REVIEW.md
test -f docs/SMOKE_PATH.md
test -f docs/VERIFICATION.md
test -f scripts/verify-contracts.sh
test -f tests/contracts/contracts_test.go
```

The current contract-test scaffold lives under `tests/contracts/` and is exercised through `scripts/verify-contracts.sh`.

## Expected Go verification once skeleton code exists

### Contract scaffold (available now)

```bash
./scripts/verify-contracts.sh
```

### Format

```bash
gofmt -w ./cmd ./internal ./tests
```

### Unit + package tests

```bash
go test ./...
```

### Race-sensitive pass

```bash
go test -race ./...
```

### Static sanity

```bash
go vet ./...
```

## High-value test targets

Prioritize these behaviors first:

1. request submit persists `request_created`
2. projection rebuild reproduces latest state from only the event log
3. approve path requires review-socket trust rules
4. execute path rejects anything outside `approved`
5. second execute call is idempotent and does not re-run the command
6. recovery from dangling `executing` writes a failure-style repair event instead of replaying work

## Suggested smoke command order

Use this order as the codebase grows:

```bash
./scripts/verify-contracts.sh
go test ./...
go test -run Smoke ./...
```

The first step validates the frozen JSON fixture contracts under `tests/contracts/`.
Then run the manual smoke path from `docs/SMOKE_PATH.md` against a temporary runtime directory once `cmd/lb` and `cmd/lbd` exist.

## Verification evidence template

Record evidence in this format when closing a task:

```text
Verification:
- PASS docs presence: <command> -> <result>
- PASS format: <command> -> <result>
- PASS tests: <command> -> <result>
- PASS vet: <command> -> <result>
- PASS smoke path: <command> -> <result>
```

If a check is not yet applicable, say so explicitly instead of silently skipping it.
