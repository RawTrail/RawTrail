## Why

The lineage columns stored by `process-lineage-rows` leave `parent_process_id` unset, so the tree still infers a parent from `ppid` plus time (#1280, epic #1275). That misattributes a child when its parent's pid is reused, and it cannot follow a daemon once launchd adopts it. Exits bind by pid alone, so a late exit can close a newer process on a reused pid.

## What changes

- A fork that carries `parent_pidversion` links its row to the exact `(parent_pid, parent_pidversion)` row. When no row holds that generation, because the parent's first exec rewrote it or an exec was dropped, the link falls back to the parent's row in force at the fork, unless that row is a newer generation. `pidversion` restarts at each boot, so every match is bounded by time.
- A process first seen at exec links to the parent its exec names, unless launchd adopted it, whose creator the exec does not identify.
- A re-exec keeps the link. A parent created in the same batch is linked after the batched insert, so the flush stays set-based.
- An exit that carries `pidversion` closes its own generation's row, or an older or unversioned one, and never a newer one. A buffered exit is not taken by a snapshot of a newer generation.
- Events from older agents keep today's behavior.

## Impact

- `server/detection/internal/mysql/{processes,processbatch}.go` and `server/detection/internal/graph/{builder,batch}.go`. No migration and no API change: the tree read uses the link in the next PR.
