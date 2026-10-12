## Why

On a developer Mac an AI agent and its human share the account, the shell and the binaries, so exec telemetry shows that a process ran but not which agent session caused it (#1278, epic #1275). Claude Code exports `CLAUDE_CODE_SESSION_ID` into every process it spawns, and the value is the name of the session's transcript file. The variable is inherited, so one exec attributes a whole subtree, including processes reparented to launchd that lineage alone loses.

The spike in #1276 showed that the markers survive `nohup`, double forks, `disown`, `setsid` and `osascript`, and that `sudo`, `env -i` and Homebrew strip them. The same environment holds live credentials: `CLAUDE_CODE_MESSAGING_TOKEN` shares the `CLAUDE_CODE_` prefix with the session id.

## What Changes

- `exec` payloads carry an optional `env` object holding only the allowlisted agent session markers, matched on the full variable name: `CLAUDE_CODE_SESSION_ID`, `CLAUDE_PID`, `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SESSION_ATTENDED` and `AI_AGENT`.
- A value longer than 256 UTF-8 bytes is dropped, not truncated, because a truncated session id would join the wrong session.
- `env` is omitted when no marker is present, so the common exec shape is unchanged. The server ignores it until #1281 stamps agent sessions at ingest.

## Impact

- `extension/edr/extension/AgentSessionEnv.swift` (new), `EventSerializer.swift`, `ESFSubscriber.swift`, and `schema/events.json`.
- Agent-spawned execs grow by about 250 bytes; other execs are unchanged.
