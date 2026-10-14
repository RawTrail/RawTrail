## Why

The extension reports what an AI agent's processes do to files as `file_activity` events (`agent-file-activity`, #1287), but nothing reads them: the activity view's Files tab lists only watched-path events, and the host timeline shows exec, network and DNS only.

## What changes

- The activity rollup reads `file_activity` beside the watched-path file events. A row's operation is the agent's (create, modify, rename, delete), a folded event weighs its `coalesced_count` and ends at its `last_timestamp_ns`, and a rolled-up event is listed as the directory it stands for, marked `rolled_up`.
- A session's activity includes the agent process itself, the creator of its tool calls, so Claude Code's own Edit and Write show beside its shell commands' changes.
- The host timeline gains a Files class for agent file activity, with each row spelling out the operation, its path, and how many operations it folds.
- The Files tab marks a rolled-up row as a directory and everything below it.

## Impact

- `server/visibility/internal/clickhouse/activity.go`, `server/visibility/api/{eventarchive,eventsearch}.go`, the OpenAPI spec.
- `ui/src/components/Activity/ActivityTabs.tsx`, `ui/src/components/HostTimeline.tsx`, `ui/src/types.ts`, and their tests.
- The fake agent sends `file_activity`; the Claude Code scenario and journey gain git's index update.
