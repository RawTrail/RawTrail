## Why

The sessions list (#1283) needs every AI agent session on a host with when it started, when it was last active and where it was working. Deriving that from `processes` would mean grouping every stamped row on each read (#1281).

## What changes

- Migration 00020 adds `agent_sessions`, keyed by host and session: agent name and version, the agent's pid, the cwd of the earliest tool call seen, and first and last seen.
- The graph builder upserts it at flush from every exec carrying a session marker. First and last seen only move outward. The pid, cwd, name and version come from the earliest sighting, so batches arriving out of order and replays leave the same row.
- A `CLAUDE_PID` too large for the column is dropped from the sighting rather than failing the batch.

## Impact

- `server/detection/migrations/00020_agent_sessions.sql`, `server/detection/internal/mysql/processbatch.go`, `server/detection/internal/graph/{builder,batch}.go`. No read yet: #1283 lists the table.
