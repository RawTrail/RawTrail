## Why

The epic's goal is to show what an AI agent did (#1275). The extension now forwards the agent's session markers on exec (#1278), and the server links each process to its creator (#1280), but nothing groups processes by agent session. A reader would have to walk every process tree and re-derive the session from markers that `sudo`, `env -i` and brew strip (#1281).

## What changes

- Migration 00019 adds `agent_session_id`, `agent_name`, `agent_version`, `story_root_id` and `tool_call_group` to `processes` as one instant ALTER, plus indexes on `(host_id, agent_session_id)` and `(host_id, story_root_id)` built online. The last two columns are filled by a later change of #1281.
- An exec carrying `CLAUDE_CODE_SESSION_ID` stamps its row with that session, and with the agent's name and version from `AI_AGENT`. A marker over 256 bytes is ignored.
- Every other row inherits: a fork takes its creator's session, an exec without the marker keeps what the row inherited, and a re-exec keeps the prior generation's. A new marker replaces an inherited session, so `/clear` starts a new one.
- A row with no creator and no marker has no session.

## Impact

- `server/detection/migrations/00019_processes_agent_session.sql`, `server/detection/api/types.go`, `server/detection/internal/mysql/{processes,processbatch}.go`, `server/detection/internal/graph/{builder,batch}.go`.
- `api.Process` gains optional JSON fields; existing clients ignore them.
