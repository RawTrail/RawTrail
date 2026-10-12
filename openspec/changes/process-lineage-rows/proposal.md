## Why

The process tree infers a parent from `ppid` plus time, which misattributes under pid reuse and breaks once a daemon is reparented to launchd (#1280, epic #1275). The extension now sends the identity needed to do better (#1277): the fork's `parent_pidversion` names the exact creator, and the exec reports `original_ppid`, the responsible process, the session and process group, the terminal, the start time and the working directory. The server drops all of it.

This change stores those fields. Resolving `parent_process_id` from them, binding exits by `pidversion`, and reading the tree by exact edges follow in later PRs of #1280.

## What changes

- Migration 00018 adds `parent_process_id`, `parent_pidversion`, `original_ppid`, `responsible_pid`, `responsible_pidversion`, `session_id`, `group_id`, `tty`, `start_time_ns`, `cwd` and `reparented` to `processes`, as one instant ALTER.
- A forked row stores the fork's `parent_pidversion`. An exec stores its lineage on the row it images and marks `reparented` when launchd adopted the process. An exec never changes the row's `ppid` or `parent_pidversion`, because it names the current parent, not the creator.
- A row first seen at exec stores the exec's `ppid` with its `parent_pidversion`, so the two stay a pair. A re-exec keeps the prior generation's parent link.
- Events without the fields leave the columns unset, so older agents build the same rows as before.

## Impact

- `server/detection/migrations/00018_processes_lineage.sql`, `server/detection/api/types.go`, `server/detection/internal/mysql/{processes,processbatch}.go`, and `server/detection/internal/graph/{builder,batch}.go`.
- `api.Process` gains optional JSON fields; existing clients ignore them.
