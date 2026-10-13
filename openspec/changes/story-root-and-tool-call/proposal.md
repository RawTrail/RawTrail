## Why

The agent session (`agent-session-stamping`) answers "what did this agent do", but two narrower questions need their own keys (#1281). One is the whole story a process belongs to, from the process launchd started down. The other is which tool call started a process. Without stamped keys, both mean walking parent links at read time.

## What changes

- `story_root_id` is the row of the nearest ancestor launchd started. A process whose fork-time parent is launchd is its own root. So is a process first seen at exec whose parent is launchd and that was not reparented. Every other row takes its creator's root, and a re-exec keeps the prior generation's. A row with no creator has none.
- `tool_call_group` is the process group of the agent's direct child. An exec whose creator's pid equals its `CLAUDE_PID` marker starts a tool call with its own group. Every other row inherits it like the agent session, so it survives `setsid`, which changes the process group itself.
- The flush resolves a root created in the same batch, including a row that is its own root, after the batched INSERT, together with same-batch creator links.

## Impact

- `server/detection/api/types.go`, `server/detection/internal/mysql/{processes,processbatch}.go` and `server/detection/internal/graph/{builder,batch}.go`. No migration: 00019 added the columns.
