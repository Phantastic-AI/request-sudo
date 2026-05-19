# Issue tracker: Hub tasks (project `RSUDO`)

Issues and PRDs for this repo live as Hub tasks under project key `RSUDO` ("request-sudo"). Use the `hub` CLI for all operations.

The Hub instance runs at `http://localhost:3001` (a.k.a. `http://discovery-two:3001`). Vault is at `/home/openclaw/.openclaw/workspace/hub/vault`. The `hub` binary is on `PATH` for all users via `/usr/local/bin/hub`.

## Conventions

- **Create an issue**: `hub task create "Title" --project RSUDO [--description "..."] [--priority low|normal|high]`. The `--description` value supports multi-line strings via shell quoting.
- **Read an issue**: `hub task show <id>` (numeric ID, with or without `T` prefix).
- **List issues**: `hub task list --project RSUDO` (default: open). Add `--status closed` or `--all` for other states.
- **Comment on an issue**: `hub task comment <id> --body "..."`
- **Apply a label**: `hub task label <id> --add <label>` / `--remove <label>`
- **Close**: `hub task close <id>` (optionally `--body "..."` first via comment, then close)
- **Search**: `hub task search "query"` (matches title + description across the whole vault, not project-scoped)
- **Block / unblock**: `hub task block <id> --by <other-id>` to set dependencies.

## When a skill says "publish to the issue tracker"

Create a Hub task under `RSUDO`:

```bash
hub task create "Title" --project RSUDO --description "$(cat <<'EOF'
Body...
EOF
)"
```

## When a skill says "fetch the relevant ticket"

Run `hub task show <id>` and inspect the JSON output. Comments come back nested under `comments`. Labels under `labels`.

## When a skill says "list issues with label X"

`hub task list --project RSUDO` then filter the JSON by label, or use `hub task label <id>` patterns. (Hub's CLI doesn't yet have a server-side label filter — filter client-side with `jq`.)

## Notes

- Hub is the live work-tracking system on this pod. Avoid creating loose `.md` files in this repo to track work — use Hub instead so it's discoverable across all OpenClaw agents and the team.
- If a Hub CLI command behaves unexpectedly, check `hub <domain> --help` first; bugs/gaps in the CLI itself are tracked under project `HUB`.
- This repo is local-only with no git remote, so issue tracking and code tracking are separate.
