## Why

A session's activity includes the agent process itself, the creator of its tool calls (`agent-file-activity-views`, #1318), but one `claude` process can serve several sessions in turn: `/clear` and `/resume` switch the session without a new process. Measured on edr-dev, one process served two sessions across a `/clear`, and each session's activity showed the agent's files and connections from both (#1319).

## What changes

- The activity rollup can bound a generation to a span of time, counting its events only inside it.
- A session's activity counts the agent process only while it served the session. That runs from its first touch of a path Claude Code names after the session (its transcript, or the session's scratch or `session-env` directory) to its first touch of a path of a later session. The first session the process served also keeps the process's start.
- When the agent touched no other session's paths, or never one of this session's, it is counted whole, as before.

## Impact

- `server/visibility` (the span predicate and a session-touch read, in ClickHouse and the in-memory fake), `server/detection/internal/operator/activity_handler.go`, and their tests.
