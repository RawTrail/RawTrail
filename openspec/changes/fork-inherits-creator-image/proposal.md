## Why

A forked child that has not exec'd runs its parent's image, and the graph builder records that image as the child's: path and signing identity. It picked the image by time, as the newest generation of the parent's pid to have forked by the child's fork. Since #1280 the fork names its creator exactly, by `(parent_pid, parent_pidversion)`, and under pid reuse the two can disagree. On the dev server, a child linked to its real creator (zsh) inherited the path of the process that reused the pid (python3) (#1298).

## What changes

- When the fork's creator was resolved, the child inherits that row's image. Exec bumps the pidversion, so the row is the image the creator was running when it forked. The signing identity is kept only if it was in force at the fork, by the rule the time-based lookup applies.
- A fork that names no creator, or whose creator was not found, inherits by time as before.

## Impact

- `server/detection/internal/graph/builder.go`, and a detection integration test.
