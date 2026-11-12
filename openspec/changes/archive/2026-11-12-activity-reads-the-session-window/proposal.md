## Why

On dogfood (v0.9.0-rc.7) a 10-day AI agent session's Activity tab still took about 4 s to show its first list (issue #1550). Every read of the session covered the laptop's last 20 days, not the session's 10: the read started at the earliest start of the session's processes, and the Claude Code process that served it had started 11 days before, for an earlier session. Each read also looked up that process's session touches, a 0.3 s scan of its file events with no time bound, again on every request.

## What changes

- A session's activity is read from the start of the span each of its agents served it, where the agent served an earlier session first, rather than from the agent's own start. An event that stands for a stretch of time is still kept when its stretch overlaps the session.
- Each replica keeps an agent's session touches for 30 seconds and shares one lookup between the reads that need it at once.

## Impact

- `server/detection/internal/operator`: `scopeStart` takes the session's agent spans; a generic kept-read cache serves both the session's process read and its agents' touches.
- `server/visibility/internal/clickhouse`: the read's lower bound is judged by the same expression as each span's start.
- No API or wire change.
