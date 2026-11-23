## Why

One AI agent session could raise two `agent_credential_read` alerts for the same kind of credential, though the rule promises one per session and kind (#1589). The rule grouped a read under its session only when the reading process carried the session's marker. Claude Code sets those markers only on the commands it runs, so the agent's own process has none, and its read was alerted on its own beside the session's alert. Seen on edr-dev with a v0.9.0 agent and in the #1588 demo seed: a protect-mode `claude -p` session raised one alert for `claude` and another for its `ugrep`.

## What changes

- A read by a process with no session marker joins the session that process leads as the agent: the latest session recording its pid as the agent's, first seen during the process's own lifetime and at or before the read. Its read becomes evidence on the session's alert.
- A read by a process in no session and leading none is still alerted on its own.
- The rules' graph reader gains `GetAgentSessionLedBy`, wrapped for retry and timing like every other read.

## Impact

- `server/rules/internal/catalog/agent_credential_read.go`, `server/detection/api/service.go`, `server/detection/internal/mysql/agent_sessions.go`, `server/detection/internal/engine/{graphreader,timedreader}.go`.
- No migration: the lookup reads `agent_sessions` by its `host_id` key prefix.
- Residual: an agent that reads a credential before running any command has no session recorded yet, so that read is still alerted on its own.
