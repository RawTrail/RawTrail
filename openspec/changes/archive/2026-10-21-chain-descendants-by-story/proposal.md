## Why

The alert's chain read walks descendants one query per process and stops at 500 (#1282). A Claude Code session easily passes that, and the walk's cost grows with the subtree. #1281 stamped each process with its story root, indexed with fork time (migration 00021), so a chain's candidate descendants are one index read.

## What changes

- When the pinned process carries a story root, the chain read fetches the candidates in one indexed query. Those are the story's processes and the unstamped processes forked between the pinned process's fork and the window's end. An unstamped one is a child forked before its creator's row was recorded, which the builder leaves without a story.
- It walks the candidates in memory with the walk's own rule, `ppid` and the parent's lifetime plus the creator-link check, so the chain matches what the walk returns, without the 500 cap.
- A process without a story root, or a story with more than 20,000 candidates (one rooted at an IDE can be most of a host), walks with a query per process as before.

## Impact

- `server/detection/internal/graph/chain.go`, `server/detection/internal/mysql/scoped_tree.go`. No API change.
