---
status: accepted
date: 2026-05-01
---

# Canon storage format and integrity guarantees

The canon (the append-only event log of every Request lifecycle event) is the foundation of request-sudo. Every other component derives state from it. This ADR locks down the on-disk format, hash-chain semantics, durability protocol, locking, recovery rules, logical sequencing, output capture, and execution timeout. The decisions are interlocking; they land as one ADR rather than several because separating them would create false independence.

## Test plan (must all be red before any implementation)

This ADR is contract-test-driven: every assertion below is a test the implementation must pass. New behavior added later requires new tests + an ADR amendment.

### Canonicalization (encoding)
- **T1.** Two independent implementations of RFC 8785 JCS produce byte-identical encodings for the same event object (cross-encoder fixture: 50 representative events).
- **T2.** An event with a duplicate JSON key is rejected at write time with `canon: duplicate key`.
- **T3.** An event with a JSON float/decimal in any field is rejected with `canon: floats not allowed`.
- **T4.** Strings that traverse the digest are normalized to NFC at write time; mixed NFD input round-trips to NFC; verifying a chain that contains NFD bytes (manually injected) fails with `canon: non-NFC string`.
- **T5.** Empty events and events without all required fields (`schema_version`, `seq`, `wall_ts`, `boot_id`, `mono_delta_us`, `type`, `request_id`, `actor`, `prev_hash`) are rejected at write time. `request_id` may be the JSON value `null` for system events (e.g. `recovery_truncated_partial_event`); `actor` may not.

### Durability

The crash-recovery tests must accept *any* of the legal outcomes of an interrupted write, since `SIGKILL` may strike before the byte stream reaches the page cache, between page cache and disk, or after disk durability is achieved. Each crash test verifies that recovery handles the *post-crash filesystem state*, not a specific predicted state.

- **T6.** Broker killed with `SIGKILL` mid-append (between starting the write and HEAD rename): post-crash file may show old EOF (write didn't reach disk), partial line (write reached fs but not flushed), or full valid line (write fully landed). Recovery accepts each: old EOF → no event added (request will be retried by client); partial line → truncate at last newline + emit `recovery_truncated_partial_event`; full valid line + missing HEAD update → rebuild HEAD from scan.
- **T7.** Broker killed after `fsync(file)` on an *existing* monthly file but before `fsync(canon_dir)`: line is durable. (For first-write-to-new-month, the directory entry is not durable until `fsync(canon_dir)` — recovery handles that case under T7b.)
- **T7b.** Broker killed during the *first* write to a new monthly file, after `fsync(canon_fd)` but before `fsync(canon_dir)`: directory entry may not be durable; canon_dir may show the file or not on remount. Recovery scans canon_dir, ignores any zero-byte file, and continues with the most recent month present.
- **T8.** Broker killed during HEAD update (after temp write, before atomic rename): old HEAD remains; recovery rebuilds HEAD by full canon scan, never trusts cached HEAD blindly.
- **T8b.** HEAD file is rewritten by tampering (operator or attacker edits HEAD): recovery rebuilds HEAD from canon scan; tampered HEAD is silently corrected. Note: this means HEAD-based truncation detection is best-effort — see Recovery rules below for stronger detection via canon-scan invariants.
- **T9.** Broker killed during `--init` after partial genesis write but before install-witness write: recovery refuses to start, requires operator to clean up canon dir and re-run `--init`.

### Locking
- **T10.** Two simultaneous `request-sudod --init` processes: exactly one acquires the daemon lock + commit lock and writes genesis + install-witness; the other exits with `canon: already initialized` (exit code 2).
- **T11.** `request-sudoctl canon verify` runs while broker is mid-write: verify takes NO lock and tolerates a partial-tail line as a normal recovery case (a fully canonical line by definition ends with `\n`; verify treats a tail without `\n` as "in-flight, ignore"). Broker write is never blocked by verify.
- **T12.** Lock files (`canon/daemon.lock` and `canon/.lock`) are opened with `O_NOFOLLOW`; symlinks at either path cause startup failure.
- **T13.** Two simultaneous broker daemons (e.g. systemd misconfig) both try to start: the second cannot acquire `canon/daemon.lock` (held for the first daemon's entire lifetime) and exits with `canon: another broker is running`.
- **T14a.** Broker holds `canon/daemon.lock` LOCK_EX for its entire process lifetime, NOT per-commit. Releasing happens only on broker exit.
- **T14b.** Broker holds `canon/.lock` LOCK_EX briefly during each commit (lines + HEAD update + fsyncs). Releasing happens after each commit completes.
- **T14c.** `request-sudod --init` takes both `canon/daemon.lock` and `canon/.lock` LOCK_EX for the duration of init; releases both before exit.
- **T14d.** Verifier starvation: 100 verifiers running back-to-back must not prevent broker writes from completing within bounded latency (since verifier doesn't lock, this is trivially true; the test asserts the property explicitly so future "fix" attempts don't reintroduce a verifier lock).

### Hash chain
- **T15.** Tamper any byte in any prior event: `request-sudoctl canon verify` reports the offending file + byte offset and exits non-zero.
- **T16.** Insert a synthetic event mid-chain (preserving prev_hash field manually): verify catches the prev_hash mismatch with the *next* event.
- **T17.** Genesis event has `prev_hash` = 32 zero bytes hex; events with any other prev_hash at seq=1 are rejected.
- **T18.** Hash chain continues across monthly rotation boundaries: last event of month N is the prev_hash of first event of month N+1; verify walks across files.

### Recovery
- **T18.** Missing canon dir + missing `/etc/request-sudo/install-witness` + no `--init` flag: broker refuses to start with operator message pointing at `--init`.
- **T19.** Missing canon dir + existing install-witness: broker refuses to start with `canon: missing but install-witness present — possible tampering`.
- **T20.** install-witness exists but its hash does not match the genesis event's hash in canon: broker refuses to start with `canon: install-witness mismatch`.
- **T21.** Tail truncation that erases a fully-completed (non-partial) line, detected by HEAD pointing past the new EOF: broker refuses to start, no auto-recovery, operator must run `request-sudoctl canon verify`.
- **T22.** Empty canon dir but no `--init` flag: hard refuse with same message as T18.

### Logical sequencing
- **T23.** Wall-clock regression between consecutive events (next event's `wall_ts` < previous event's `wall_ts`): broker emits `clock_regression_observed` event with both timestamps before continuing.
- **T24.** `seq` field monotonically increases across the entire chain, including across monthly rotation boundaries.
- **T25.** `boot_id` (UUIDv7) is generated once per broker process; all events from one process share it; restart produces a different `boot_id`.
- **T26.** `mono_delta_us` is monotonic within a `boot_id`; verify rejects a chain where `mono_delta_us` decreases between two events sharing the same `boot_id`.

### Capture modes
- **T27.** `capture_output: interactive` (default for any requester not explicitly set): synchronous `request.execute` response includes full stdout/stderr; canon event has `output_captured: false` and no stdout/stderr fields; subsequent `request.status` poll returns `exit_code` and `duration_ms` only.
- **T28.** `capture_output: plain`: canon event has `output_captured: true` with stdout/stderr (truncated at 64KB each, with `stdout_truncated`/`stderr_truncated` boolean markers); both synchronous execute response and later `request.status` poll return them.
- **T29.** `capture_output: encrypted-to-approvers` in policy file at v1 startup: broker refuses to start with `policy: capture mode 'encrypted-to-approvers' reserved for v2`.
- **T30.** Capture truncation at 64KB: stdout > 64KB is stored as exactly 64KB + `stdout_truncated: true`; same for stderr.
- **T30a.** Binary stdout (non-printable bytes including NUL): captured bytes are stored as a base64 string in canon when capture is `plain`; canon event includes `stdout_encoding: "base64"` field; UTF-8 stdout is stored as a JSON string with `stdout_encoding: "utf8"`.
- **T30b.** Non-UTF-8 byte sequences (invalid UTF-8) in stdout: detected at capture time; broker switches that stream to base64 encoding rather than mangling the bytes through replacement characters.
- **T30c.** Interleaved stdout/stderr ordering: separate pipes for each; canon event records each independently (no merged "output" field). Test asserts both streams' bytes are intact even when both produce data simultaneously.
- **T30d.** Requester disconnect mid-execute (synchronous mode only): broker frees in-memory output buffers within 100ms of detecting socket EOF (no leak via long-lived dead requests).
- **T30e.** Output written between SIGTERM and SIGKILL (during the 2s grace): captured if any; included in `execution_killed_timeout` event under `stdout`/`stderr` if `capture_output: plain`. If `capture_output: interactive`, the requester is no longer waiting (timeout already triggered) — output is discarded.

### Execution timeout
- **T31.** Approved command runs longer than per-requester `max_execution_seconds`: broker first sends `SIGTERM` to the entire process group; waits 2 seconds; then sends `SIGKILL` to the process group. Emits `execution_killed_timeout` event with elapsed time and which signal (TERM or KILL) was effective. Request transitions to `failed`.
- **T32.** Per-requester `max_execution_seconds` unspecified in policy: 300-second default applies.
- **T33.** Per-requester `max_execution_seconds > 3600` (the global hard cap): broker refuses to start with `policy: max_execution_seconds exceeds hard cap (3600)`.
- **T33a.** Per-requester `max_execution_seconds <= 0`: broker refuses to start with `policy: max_execution_seconds must be positive`.
- **T33b.** Per-requester `max_execution_seconds` not an integer (float, string, missing): broker refuses to start with `policy: max_execution_seconds must be a positive integer`.
- **T34.** A command that exits cleanly within the cap: no `execution_killed_timeout` event; normal `execution_succeeded` is emitted.
- **T34a.** Process tree: child spawns descendants (e.g. `bash -c "sleep 9999 &"`). On timeout, broker kills the *entire process group* via `kill(-pgid, SIGTERM/SIGKILL)`; the orphaned `sleep` is killed too. Test asserts no descendant survives.
- **T34b.** Broker creates a new session for the child via `setsid()` before execve, ensuring the child becomes the leader of its own process group. Test verifies via /proc/<pid>/stat that the child's pgid equals its pid.
- **T34c.** SIGTERM grace cannot extend execution beyond the cap+2s. If the child handles SIGTERM and continues running, the SIGKILL fires at cap+2s exactly.

## Decision

### Storage layout

```
/var/lib/request-sudo/
├── canon/                              owner root:root  perms 0700
│   ├── 2026-05.jsonl                   monthly canon files
│   ├── 2026-06.jsonl
│   ├── HEAD                            cached pointer: {hash, file, byte_offset, seq}
│   ├── daemon.lock                     held LOCK_EX for entire broker process lifetime; opened O_NOFOLLOW
│   └── .lock                           held LOCK_EX briefly per commit; opened O_NOFOLLOW
└── /etc/request-sudo/
    └── install-witness                 sha256 of genesis event line, 0600 root:root
```

Monthly file selection is determined by the **event's wall_ts** (assigned under writer lock), not the broker's current wall-clock at write time. Late events with previous-month timestamps still go into the previous-month file (which is unsealed in this design — a file is "current" until rotation; rotation happens on first write of a new month).

### Encoding

Events are encoded as **RFC 8785 JSON Canonicalization Scheme (JCS)** with these additional restrictions:
- Integers only — no floats, no decimals, no scientific notation
- Integer fields that may exceed IEEE-754 safe range (`mono_delta_us` for long-running brokers) are **encoded as decimal strings** to bypass JSON's ~2^53 safe-integer limit. `seq` is bounded to 2^53−1 (sufficient for ~285,000 events/sec for a year — orders of magnitude beyond expected use)
- `wall_ts` is encoded as an RFC 3339 UTC string (already string)
- UTF-8 strings only, NFC-normalized at write time
- Duplicate keys are rejected at parse and write
- Required top-level fields: `schema_version` (integer, currently `1`), `seq`, `wall_ts` (RFC 3339 UTC string), `boot_id` (UUIDv7 string), `mono_delta_us` (decimal string), `type`, `request_id` (string or null), `actor`, `prev_hash` (sha256 hex), plus type-specific fields under `details`

`schema_version` is on every event (top-level), so verifiers know which schema rules apply per-event. Adding fields under `details` does not require a schema bump; changing top-level field semantics or removing a field does. There is no separate "schema marker" event — that earlier proposal was rejected because it conflicted with chain-membership rules.

One event = one JCS-canonicalized JSON object + a single `\n` terminator. The terminating newline is included in the byte range hashed.

### Hash chain

`prev_hash` of an event is `hex(sha256(serialized_line_of_previous_event))`, where `serialized_line` is the full canonical JSON object (including its own `prev_hash` field) followed by `\n`. Genesis prev_hash is 32 zero bytes hex.

This makes the chain bind the bytes-as-written. Re-encoding any event with a different (but semantically equal) JSON layout would change its hash, but JCS plus the listed restrictions guarantees a single canonical form, so this is safe.

### Durability protocol

For each event the broker writes:
1. Acquire exclusive `flock` on `canon/.lock`
2. Write the JCS line + `\n` to the active month file (currently open with `O_APPEND`)
3. `fsync(canon_fd)`
4. Write `canon/HEAD.tmp` with the new HEAD record (hash, file, byte_offset, seq)
5. `fsync(HEAD.tmp)`
6. `rename(HEAD.tmp, HEAD)` (atomic on POSIX)
7. `fsync(canon_dir)` to ensure the rename is durable
8. Release lock

Recovery never trusts HEAD. On startup, broker scans the latest canon file (and earlier files if HEAD points at them), recomputes the trailing event's hash, rebuilds HEAD from scratch, and only after that successful rebuild does it become the authoritative HEAD. The `HEAD` file is a hot-path cache, not canonical state.

### Locking

Two-lock model. Both lock files opened `O_NOFOLLOW` to defeat symlink swaps.

**`canon/daemon.lock`** — broker process-lifetime lock.
- Broker daemon: takes `LOCK_EX` non-blocking on startup; holds for the entire process lifetime; releases on exit.
- Second broker daemon starting (e.g. systemd misconfig): cannot acquire; exits with `canon: another broker is running`.
- `request-sudod --init`: also takes `LOCK_EX` on `daemon.lock` for the init duration; cannot run while broker is up.
- `request-sudoctl` and verify: do NOT touch this lock.

**`canon/.lock`** — per-commit serialization.
- Broker daemon: takes `LOCK_EX` briefly during each commit (steps 1–8 of durability protocol); releases after each commit.
- `request-sudod --init`: also takes `LOCK_EX` during init; ensures genesis write is serialized.
- `request-sudoctl canon verify`: does NOT take this lock. Verify reads canon files directly. Canon files are append-only, so verify tolerates seeing a partial-tail line (treats it as an in-flight write to skip — same logic as recovery's truncated-tail handling). This avoids verifier-vs-writer starvation entirely.

**Why no verify lock:** an earlier draft had verify take `LOCK_SH` while broker takes `LOCK_EX`. That created two problems: (1) repeated verifiers could starve the broker writes, and (2) it forced broker writes to wait for verify reads to finish, which contradicted "broker not blocked on append." The append-only property of canon makes a lock unnecessary: a partial-tail line is detectable (no terminating newline), and verify can skip it just like recovery does.

### Recovery rules

| Situation | Action |
|---|---|
| Fresh install (no canon dir, no install-witness, `--init` passed) | Acquire lock, write genesis event with prev_hash=zero, write install-witness, fsync everything, exit 0 |
| Missing canon dir AND no install-witness AND no `--init` | Hard refuse; print "fresh install? run request-sudod --init" |
| Missing canon dir AND install-witness present | Hard refuse; print "canon missing but install-witness present — possible tampering" |
| Empty canon dir (exists but no .jsonl files) | Hard refuse; same as missing canon |
| install-witness mismatch with genesis event hash | Hard refuse; print "install-witness mismatch — possible tampering" |
| Truncated last line (last bytes are not valid JCS or no terminating `\n`) | Truncate at last newline, emit `recovery_truncated_partial_event` with pre/post byte counts, continue |
| Tail truncation past HEAD's recorded byte_offset (a complete line vanished) | Hard refuse; operator must run `request-sudoctl canon verify` |
| Hash chain broken at any point during verify | Hard refuse; no auto-repair |

### Logical sequencing

Every event includes:
- `seq`: monotonically increasing integer, persistent across restarts (broker reads HEAD's seq on startup and resumes from there)
- `wall_ts`: RFC 3339 UTC timestamp, audit metadata
- `boot_id`: UUIDv7 generated once per broker process
- `mono_delta_us`: microseconds since `boot_id` was generated, from `CLOCK_MONOTONIC`

Wall-clock regressions are tolerated but flagged: when a new event's `wall_ts` would be less than the previous event's, the broker first emits a `clock_regression_observed` event and then proceeds. Verify warns on these but does not fail the chain (clock regressions are ops noise, not tamper evidence).

### Output capture modes

A new field on each routing entry in the approver policy file (ADR-0002):
```yaml
routing:
  openclaw:
    approver_set: operators
    capture_output: plain          # canon stores stdout/stderr (truncated 64KB)
    max_execution_seconds: 600
  aditya:
    approver_set: operators
    capture_output: interactive    # default; output via synchronous response only
  diag-bot:
    approver_set: operators
    capture_output: plain
```

v1 broker accepts: `interactive` (default for any unspecified entry), `plain`. Reserved-and-rejected: `encrypted-to-approvers`, `summary`. Rejected at startup with a clear policy error (does not silently downgrade).

`request.execute` always streams stdout/stderr in its synchronous response while the broker holds them in process memory. After the response is sent (or 5-minute deadline if the requester disconnects), in-memory output is freed. `request.status` returns stdout/stderr only when the canon event has `output_captured: true`.

### Execution timeout

Per-requester `max_execution_seconds` (soft cap) defaults to 300 (5 minutes) when unspecified. Global hard cap is 3600 (1 hour), compile-time const, non-overridable. Policy entries setting `max_execution_seconds > 3600` cause broker startup failure.

Before `execve`, broker calls `setsid()` so the child is the leader of its own process group and session. When the child exceeds its cap: broker sends `SIGTERM` to the **entire process group** via `kill(-pgid, SIGTERM)`, waits 2s, then `SIGKILL` to the same process group via `kill(-pgid, SIGKILL)`. This ensures any descendants the child spawned (e.g. a backgrounded `sleep`) are also killed, not orphaned. Emit `execution_killed_timeout` event with elapsed time and which signal was effective. Request state transitions to `failed`.

## Considered options

### Canonicalization
- **Sorted-keys JSON** (rejected): Underspecified — encoders disagree on number formatting, Unicode normalization, escape choices. JCS is a published RFC with conformance tests.
- **Custom binary format** (rejected): Reinvents the wheel; harder to inspect; no win at expected event volumes.
- **CBOR** (rejected): Smaller and faster, but loses ad-hoc inspectability that the literate-broker positioning requires.

### Durability
- **No fsync** (rejected): Loses durability on power failure; defeats the audit-log purpose.
- **`fsync` only on file, no atomic HEAD rename** (rejected): Crash mid-update can leave HEAD pointing past durable bytes or before durable bytes — recovery becomes ambiguous.

### Locking
- **Per-process advisory locking only** (rejected): Misses the cross-process case (broker + ctl + recovery).
- **POSIX file ranges** (rejected): More fragile than `flock` and not needed for our access pattern.

### Capture modes
- **Default-on plaintext capture** (rejected): Persistent secret-leak surface for every approved command.
- **Per-request `--capture` flag** (rejected): Wrong layer of consent — the requester (an agent) shouldn't decide whether its own output is logged. Operator decides at policy file authoring time.
- **LLM-summary capture** (deferred to v2+): Inline LLM call inside root daemon is a huge new attack surface; non-deterministic so doesn't fit the digest. Could be a separate, unprivileged tool that operates on canon (read-only) — not in v1.
- **Encrypted-to-approvers capture** (reserved for v2): Genuinely good design (per-event AES-GCM, envelope-encrypt to approver pubkeys). Not v1 because it requires pubkey registration, key rotation policy, recovery story for lost approver keys.

### Execution timeout
- **No timeout** (rejected): `tail -f` and similar will hold broker resources forever.
- **Single global timeout, no per-requester** (rejected): Real workloads have very different lengths (a `restart` is seconds; a `dpkg-reconfigure` is minutes). Per-requester soft cap with a global hard cap is the right shape.

## Consequences

- **Storage cost is bounded by event count and capture policy.** A pod doing ~50 requests/day with `capture_output: plain` and 64KB caps costs at most ~3 GB/year in canon. Negligible. Ops monitoring of canon size is deferred to ops docs, not v1.
- **Future protocol extensions can add fields to events** as long as they go into `details` (an inner object) and JCS encoding remains stable. Top-level fields must not change without a new ADR and chain-format version bump.
- **Recovery refuses-to-start liberally.** This is correct for an audit log: silent self-repair erases evidence. Operators must intervene on tamper; the operator burden is the price of trustworthy canon.
- **Disk-full handling**: broker fsyncs fail loudly; broker enters a refuse-new-requests state if canon writes start failing. Operators monitor disk via standard means; v1 doesn't ship a built-in `df` watchdog.
- **Schema versioning** is per-event: every event carries a top-level `schema_version` integer. v1 broker writes events with `schema_version: 1`. A future v2 broker writes new events with `schema_version: 2` and reads both. Verifier dispatches per-event validation rules based on the event's own `schema_version`. There is no separate schema-marker event and no out-of-band schema file — every event self-describes.
- **install-witness is tamper-evidence, not tamper-resistance.** Documented in the recovery section; any deployment requiring real defense against root-already attackers needs remote canon shipping (out of scope for v1).
- **No backup/restore tool in v1.** Canon files are plain JSONL; standard `cp -a` plus the install-witness file is sufficient for cold backup. A `request-sudoctl canon export` / `import` command is follow-up work, not v1.
