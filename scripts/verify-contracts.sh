#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONTRACTS_DIR="$ROOT/tests/contracts"

required_files=(
  "$ROOT/SPEC.md"
  "$ROOT/PLAN.md"
  "$ROOT/ARCHITECTURE_OUTLINE.md"
  "$ROOT/PROTOCOL.md"
  "$ROOT/STATE_MACHINE.md"
  "$ROOT/docs/PHASE1_REVIEW.md"
  "$ROOT/docs/SMOKE_PATH.md"
  "$ROOT/docs/VERIFICATION.md"
  "$ROOT/scripts/verify-contracts.sh"
  "$ROOT/scripts/install.sh"
  "$ROOT/scripts/smoke-local.sh"
  "$ROOT/packaging/systemd/request-sudod.service.tmpl"
  "$CONTRACTS_DIR/contracts_test.go"
)

echo '[verify-contracts] checking required design + scaffold files'
for path in "${required_files[@]}"; do
  if [[ ! -f "$path" ]]; then
    echo "[verify-contracts] ERROR: missing required file $path" >&2
    exit 1
  fi
done

cd "$CONTRACTS_DIR"

echo '[verify-contracts] gofmt -w ./...'
gofmt -w .

echo '[verify-contracts] gofmt -l .'
gofmt -l . | tee /tmp/lease-broker-contracts-gofmt.txt
if [[ -s /tmp/lease-broker-contracts-gofmt.txt ]]; then
  echo '[verify-contracts] ERROR: gofmt reported unformatted files' >&2
  exit 1
fi

echo '[verify-contracts] go test ./...'
go test ./...

echo '[verify-contracts] go vet ./...'
go vet ./...


echo "[verify-contracts] smoke-local"
"$ROOT/scripts/smoke-local.sh"
