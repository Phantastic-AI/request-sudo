# Decisions

Canonical log of design decisions, deferred items, and open questions for request-sudo. **Active decisions** live in [`docs/adr/`](docs/adr/) — this file indexes them and tracks what's been deferred or is still open. Update when an ADR lands, when a deferred item is revisited, or when a new open question arises.

## Active ADRs

| ADR | Title | Date | Status |
|---|---|---|---|
| [0001](docs/adr/0001-drop-lease-terminology.md) | Drop "lease" terminology | 2026-05-01 | accepted |
| [0002](docs/adr/0002-static-file-approver-routing.md) | Static-file approver routing | 2026-05-01 | accepted |
| [0003](docs/adr/0003-sanitized-execution-context.md) | Sanitized execution context | 2026-05-01 | accepted |
| [0004](docs/adr/0004-canon-storage-format-and-integrity-guarantees.md) | Canon storage format and integrity | 2026-05-01 | accepted |
| [0005](docs/adr/0005-twilio-sms-adapter-trust-and-lifecycle.md) | Twilio SMS adapter trust + lifecycle | 2026-05-11 | accepted |

## Deferred (intentionally not in v1)

Items that are out of scope for v1 by deliberate choice. Each has a **revisit trigger** so we know when to bring it back.

| Topic | Source ADR | Revisit when |
|---|---|---|
| Verify Service SID rotation per request (to avoid global 60202 lock) | ADR-0005 follow-up | If the per-Verify-Service rate-limit becomes a real production problem. Needs a Twilio support conversation about billing implications. |
| Voice call channel (Twilio Voice) as fallback when SMS is unreliable | ADR-0005 | When SMS delivery becomes unreliable for an operator (e.g. poor cell coverage). Voice channel uses the same approval-code lifecycle. |
| Email approval channel with pubkey/2FA signing | ADR-0005 | When an operator needs an approval channel that survives both SMS and Twilio outages. |
| Push notification (Pushover/Pushbullet/APNS) channel | ADR-0005 | When operator wants a richer interactive surface than SMS. |
| `target_user` setuid path (run command as a non-root service user) | ADR-0003 | A real openclaw flow needs to run as a non-root service user (npm-fetch / python-dev / dockerbuild). Until then, root-only. |
| `--env K=V` requester-supplied environment variables | ADR-0003 | A request needs `KUBECONFIG=...` or similar that can't be set via wrapper script in argv. |
| `capture_output: encrypted-to-approvers` (per-event AES-GCM, envelope-encrypted to approver pubkeys) | ADR-0004 | Real workloads need persistent output AND canon-leak resistance is judged inadequate for v2. Needs approver pubkey registration scheme + key rotation policy. |
| LLM summary as capture mode | ADR-0004 (rejected) | If anyone wants this, build it as a separate unprivileged tool that reads canon read-only — never inline in the broker. |
| Fault-injection test harness | ADR-0004 follow-up | After v1 ships and we want to stress-test crash recovery on real hardware. |
| `approvers.yaml` schema versioning (versioned schema for the policy file itself) | ADR-0002 follow-up | When the policy schema needs a breaking change. |
| Per-requester drop-in policy directory `/etc/request-sudo/policy.d/` | ADR-0002 (rejected for now) | When there are >5 distinct requesters or per-requester options grow (per-argv-prefix policies, per-requester TTLs). |
| `request-sudoctl canon export` / `import` (backup/restore tool) | ADR-0004 follow-up | When operators need a portable canon migration path beyond `cp -a`. |
| Disk-full / inode-exhaustion handling tests | ADR-0004 follow-up | Before first production deployment. |
| Remote canon shipping / TPM sealing / signed checkpoints | ADR-0004 follow-up | If a deployment needs real defense against root-already attackers. v1 install-witness is tamper-evidence only. |
| Canon size monitoring (`hub canon size` / built-in `df` watchdog) | ADR-0004 follow-up | Operations doc work; not blocking v1. |
| **Action broker** (cross-machine RPC-style privileged actions: "pair gateway with user PC", "run X on host Y") | ADR-0001 | Separate repo, separate ADR-0001 in that repo. NOT request-sudo. |
| Streaming output during execute (chunked stdout/stderr while command runs) | ADR-0004 follow-up | When agents need live progress on long-running commands AND `capture_output: plain` polling doesn't suffice. |

## Open questions (currently unresolved)

Things still being grilled or awaiting decision. Move to ADR or deferred when resolved.

| Question | Notes | Owner |
|---|---|---|
| ~~Out-of-band approval trust model~~ | **Resolved 2026-05-11 by ADR-0005.** Twilio SMS, 6-digit numeric code, 10-min expiry, single-use, webhook HMAC-SHA1 validation, optional callback hook for deployment-specific routing (e.g. moltpod admin impersonation), no auto-retry on 429. | closed |
| `request-sudoctl` subcommand surface beyond `approve`/`deny`/`canon verify` | Listing pending? Force-cancel? Force-fail? Policy show/reload? Need an ADR for the admin tool's contract. | open |
| Phase 3 done criteria | PLAN.md lists 4 Phase 3 items (replay tamper verification, review-socket auth tightening, automated smoke path, installer + systemd unit) but testable "done" bars are not yet locked. | open |
| `setgroups` ABI: is `setgroups(0, NULL)` portable across glibc/musl/dietlibc on the platforms we'll ship? | ADR-0003 T19 assumes yes. Probably fine on Debian (glibc) but verify. | open |
| Does the broker validate that compile-time `PATH` const matches the system's actual binaries at startup, or trust it implicitly? | ADR-0003 mentions PATH as a Go const; if the system has `/usr/bin` on a noexec mount, broker should fail loudly at startup not at first execute. | open |

## Closed / superseded

(none yet — this section will accumulate as decisions get revisited)

---

_Format inspired by matt-pocock/skills (ADRs in `docs/adr/`, glossary in `CONTEXT.md`). This DECISIONS.md is our addition: a canonical place to track what was deferred and why. Read alongside [`CONTEXT.md`](CONTEXT.md) for vocabulary._
