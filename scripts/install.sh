#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PREFIX="/usr/local"
SYSTEMD_DIR="/etc/systemd/system"
STATE_DIR="/var/lib/request-sudo"
REQUEST_SOCKET="/run/request-sudo/request.sock"
REVIEW_SOCKET="/run/request-sudo/review.sock"
REVIEW_UIDS="$(id -u)"
REVIEW_GIDS="$(id -g)"
BUILD_DIR=""
DRY_RUN=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --systemd-dir) SYSTEMD_DIR="$2"; shift 2 ;;
    --state-dir) STATE_DIR="$2"; shift 2 ;;
    --request-socket) REQUEST_SOCKET="$2"; shift 2 ;;
    --review-socket) REVIEW_SOCKET="$2"; shift 2 ;;
    --review-uids) REVIEW_UIDS="$2"; shift 2 ;;
    --review-gids) REVIEW_GIDS="$2"; shift 2 ;;
    --build-dir) BUILD_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$BUILD_DIR" ]]; then
  BUILD_DIR="$(mktemp -d)"
fi
BINDIR="$PREFIX/bin"
UNIT_PATH="$SYSTEMD_DIR/request-sudod.service"

mkdir -p "$BUILD_DIR" "$BINDIR" "$SYSTEMD_DIR" "$STATE_DIR"

go build -o "$BUILD_DIR/request-sudo" ./cmd/request-sudo

go build -o "$BUILD_DIR/request-sudoctl" ./cmd/request-sudoctl

go build -o "$BUILD_DIR/request-sudod" ./cmd/request-sudod

install -m 0755 "$BUILD_DIR/request-sudo" "$BINDIR/request-sudo"
install -m 0755 "$BUILD_DIR/request-sudoctl" "$BINDIR/request-sudoctl"
install -m 0755 "$BUILD_DIR/request-sudod" "$BINDIR/request-sudod"

python3 - <<PY > "$UNIT_PATH"
from pathlib import Path
text = Path("$ROOT/packaging/systemd/request-sudod.service.tmpl").read_text()
for k,v in {
    '{{BINDIR}}':'$BINDIR',
    '{{REQUEST_SOCKET}}':'$REQUEST_SOCKET',
    '{{REVIEW_SOCKET}}':'$REVIEW_SOCKET',
    '{{EVENT_LOG}}':'$STATE_DIR/events.jsonl',
    '{{REVIEW_UIDS}}':'$REVIEW_UIDS',
    '{{REVIEW_GIDS}}':'$REVIEW_GIDS',
}.items():
    text=text.replace(k,v)
print(text, end='')
PY

if [[ "$DRY_RUN" == "1" ]]; then
  echo "dry-run complete"
fi

echo "installed request-sudo to $PREFIX"
echo "installed unit to $UNIT_PATH"
