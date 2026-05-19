---
status: accepted
date: 2026-05-01
---

# Sanitized execution context spec

The broker constructs the execution context for an approved Request itself, ignoring whatever environment the requester had. The exact context is part of the canonical digest, so any change here invalidates approvals — this spec is therefore a contract, not a tunable.

## Specification

| Component | v1 value | Configurable? |
|---|---|---|
| **PATH** | `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` | No — Go const |
| **Env allowlist** | `PATH` (above), `HOME=/root`, `USER=root`, `LOGNAME=root`, `LANG=C.UTF-8`, `LC_ALL=C.UTF-8`, `TZ=UTC` | No — Go const |
| **All other env vars** | scrubbed (no `LD_PRELOAD`, no `PYTHONPATH`, no `NODE_OPTIONS`, no SSH agent vars, no XDG_*, no proxies, nothing) | n/a |
| **cwd** | `/` | No |
| **stdin** | `/dev/null` (read-only) | No |
| **stdout / stderr** | broker-owned pipe; delivered to the requester via the synchronous response and (when policy says so) persisted to canon — see [ADR-0004](0004-canon-storage-format-and-integrity-guarantees.md) for `capture_output: interactive \| plain` | No |
| **All other fds** | closed before execve | n/a |
| **uid/gid** | `0:0` (root) | No — see `target_user` reservation below |
| **argv** | passed verbatim from the Request, no broker validation | No — see "argv is opaque" below |

## Reserved-but-not-implemented for v1

- **`target_user` field in the protocol.** Reserved for v2 — broker accepts requests only when `target_user` is null/omitted; non-null values are rejected with `error: target_user not yet supported`. v2 will add a setuid path, an approver-set policy entry per allowed target user, and inclusion of `target_user` in the canonical digest. v1 stays root-only to keep MVP scope tight.

## argv is opaque (deliberately)

The broker does **not** filter, parse, validate, or reject argv on the basis of its content. `["bash", "-c", "rm -rf /"]` will execute if approved. The literate approval flow is the trust boundary: the human sees the human-readable summary and approves or denies. Argv filtering creates false confidence (filters always have gaps) and tempts policy creep. Future readers wondering "where do we sanitize argv?" — we don't, on purpose.

## Test plan (must all be red before any implementation)

- **T1.** PATH passed to the child process is exactly `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` regardless of the broker's own PATH.
- **T2.** Env allowlist enforced: child sees exactly `PATH`, `HOME=/root`, `USER=root`, `LOGNAME=root`, `LANG=C.UTF-8`, `LC_ALL=C.UTF-8`, `TZ=UTC`. Test by approving `env` as the argv and asserting on the captured output (with `capture_output: plain` for the test policy).
- **T3.** Requester-set env vars are NOT inherited: broker is started with `LD_PRELOAD=/tmp/x.so`, `PYTHONPATH=/tmp/y`, `NODE_OPTIONS=--inspect`, `SSH_AUTH_SOCK=/tmp/z`, `XDG_CONFIG_HOME=/tmp/q`, `HTTP_PROXY=http://evil`, etc. — child process does not see any of them.
- **T4.** cwd of the child process is `/` regardless of where the broker was launched from or where the requester was when submitting.
- **T5.** stdin of the child is `/dev/null` (read returns EOF immediately).
- **T6.** stdout and stderr of the child are broker-owned pipes (verifiable by stat'ing /proc/<pid>/fd/1 and /proc/<pid>/fd/2 from the broker's perspective).
- **T7.** No file descriptors above 2 are inherited: broker is started with extra fds (e.g. fd=10 pointing to a sentinel file); child process cannot read from fd=10.
- **T8.** Child runs as uid=0, gid=0, no supplementary groups beyond the broker's own root context.
- **T9.** argv is passed verbatim: a request with `argv = ["bash", "-c", "echo $$"]` results in a child whose argv (per /proc) is exactly `["bash", "-c", "echo $$"]`.
- **T10.** argv with shell-metacharacters or other "scary" content is not filtered: a request with `argv = ["sh", "-c", "echo hello; rm -rf /tmp/test-canary"]` is accepted at submit time and produces the expected behavior at execute time (no broker-side rejection).
- **T11.** `target_user` field non-null in the request payload: broker rejects at submit time with `error: target_user not yet supported`.
- **T12.** `target_user` field absent or null: broker accepts and runs as root (the v1 default).
- **T13.** Canonical digest computed at submit time *freezes* the sanitized PATH, env allowlist, cwd, and target_user=null. At execute time the broker uses the *frozen* values bytes-for-bytes (it does not recompute from the running process's current consts). If the broker's compiled-in consts have changed between submit and execute (e.g. binary upgrade mid-flight while a request is pending), execute proceeds with the frozen values — the request behaves as it would have at submit time. The execute path therefore never produces a "digest mismatch" from runtime drift; digest mismatches only occur from explicit tampering of the request payload.
- **T14.** Empty argv (`argv = []`): broker rejects at submit time with `error: argv must contain at least one element (the executable)`.
- **T15.** argv element containing a NUL byte (`argv = ["echo", "ab\x00cd"]`): broker rejects at submit time with `error: argv element contains NUL byte`. NUL is not legal in execve argv on Linux.
- **T16.** Relative executable lookup: `argv[0] = "ls"` resolves against the sanitized PATH (`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`). The first matching binary in PATH is invoked.
- **T17.** Absolute executable path: `argv[0] = "/usr/bin/false"` is invoked directly without PATH search.
- **T18.** Missing binary: `argv[0] = "totally-not-a-real-bin"` and not found in PATH: broker emits `execution_failed` event with reason `executable not found`; canon records this; execute response surfaces the error.
- **T19.** Supplementary groups cleared: child process has zero supplementary groups (`getgroups()` returns 0). Broker explicitly calls `setgroups(0, NULL)` before execve.
- **T20.** SUID/SGID bits on the target binary: ignored — child runs with effective uid/gid from broker (root). Verifiable via /proc/<pid>/status.

## Considered options

- **Pass requester env through with a blocklist** (rejected): blocklists always miss new vars and library-specific overrides. Allowlist-by-default is the only safe posture.
- **Configurable PATH per requester** (rejected for v1): supply-chain widening surface, hard to audit. If a requester needs a binary not in the standard PATH, they invoke it by absolute path in argv.
- **Requester-supplied env via `--env K=V` flag** (deferred): no current real use case. Adding it requires extending the protocol, including env in the digest, and policy on which env keys are allowed per requester. Not v1.
- **Inherit stdin/stdout/stderr from the requester** (rejected): leaks fds, breaks reproducibility, exposes the broker to TTY-based attacks. The closed-fd-then-/dev/null pattern is standard.
- **Always capture stdout/stderr in canon** (rejected; superseded by ADR-0004): persistent secret-leak surface; ADR-0004 makes capture an opt-in policy decision per requester (`interactive` default, `plain` opt-in).
- **target_user in v1** (rejected): see ADR-0003's reservation block. v1 ships root-only; protocol field reserved for forward compat.

## Consequences

- **Tests are simple to write.** Every test that runs a command can assert exact env, cwd, and fd setup against a single Go const. Drift between test and production is mechanically detectable.
- **The canonical digest is small and stable.** PATH and env are folded into the digest as a single deterministic blob; the digest spec doesn't need to model "what env the user happened to have."
- **Captured stdout/stderr in canon has a leak surface, mitigated by ADR-0004's opt-in `capture_output` modes.** Default is `interactive` (output streams to the requester via the synchronous response only, never persisted). Operators explicitly opt specific requesters into `capture_output: plain` when they want canon to retain output (e.g. for agentic requesters that poll status later). Encrypted-to-approvers capture is reserved for v2.
- **Future protocol extension for env or target_user invalidates pre-extension digests for new fields only.** Existing digests in canon stay valid — old events keep their meaning — but the digest format gains new optional fields with explicit defaults so historical events deserialize cleanly.
- **`target_user` reservation is a forward-compatibility hedge.** Costs a few lines of validation in v1 and avoids a protocol break in v2.
