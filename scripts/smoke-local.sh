#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
REQ_SOCK="$TMP/request.sock"
REV_SOCK="$TMP/review.sock"
EVENT_LOG="$TMP/events.jsonl"
BINDIR="$TMP/bin"
mkdir -p "$BINDIR"

cleanup() {
  if [[ -n "${DAEMON_PID:-}" ]]; then kill "$DAEMON_PID" 2>/dev/null || true; wait "$DAEMON_PID" 2>/dev/null || true; fi
  rm -rf "$TMP"
}
trap cleanup EXIT

cd "$ROOT"
go build -o "$BINDIR/request-sudo" ./cmd/request-sudo
go build -o "$BINDIR/request-sudoctl" ./cmd/request-sudoctl
go build -o "$BINDIR/request-sudod" ./cmd/request-sudod

"$BINDIR/request-sudod" --request-socket "$REQ_SOCK" --review-socket "$REV_SOCK" --event-log "$EVENT_LOG" --review-uids "$(id -u)" --review-gids "$(id -g)" >/dev/null 2>&1 &
DAEMON_PID=$!

for _ in $(seq 1 100); do
  [[ -S "$REQ_SOCK" && -S "$REV_SOCK" ]] && break
  sleep 0.05
done
[[ -S "$REQ_SOCK" && -S "$REV_SOCK" ]]

REQ_JSON="$TMP/request.json"
"$BINDIR/request-sudo" request --socket "$REQ_SOCK" --reason "smoke" /bin/echo hello > "$REQ_JSON"
REQ_ID="$(python3 - <<PY "$REQ_JSON"
import json,sys
print(json.load(open(sys.argv[1]))['request_id'])
PY
)"
"$BINDIR/request-sudoctl" approve --socket "$REV_SOCK" --approver-id root "$REQ_ID" >/dev/null
OUT_JSON="$TMP/execute.json"
"$BINDIR/request-sudo" execute --socket "$REQ_SOCK" "$REQ_ID" > "$OUT_JSON"
python3 - <<PY "$OUT_JSON"
import json,sys
obj=json.load(open(sys.argv[1]))
assert obj['status'] == 'executed', obj
assert obj['stdout'] == 'hello\n', obj
PY

echo "smoke passed"
