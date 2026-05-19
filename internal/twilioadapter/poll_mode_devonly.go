//go:build dev_only

package twilioadapter

// Poll mode is an opt-in dev/test ergonomic surface for driving the
// approval flow without a publicly-reachable Twilio webhook URL.
//
// IMPORTANT: This file is gated behind the build tag `dev_only` and
// is NEVER compiled into production binaries. It does NOT satisfy
// ADR-0005 T32 (the out-of-band-channel requirement). A poll-mode
// approval is structurally indistinguishable from a local operator
// running `request-sudoctl approve <id> --code <code>` — both
// require the operator's keyboard, which a compromised box can
// scrape. Use only for dev integration tests.
//
// Add new poll-mode helpers below this comment in future PRs.

// pollModeMarker is a no-op build sentinel so this file has at
// least one symbol. Removing it triggers an "unused" build error
// in some linters; keep until real poll-mode code lands.
const pollModeMarker = "dev-only"
