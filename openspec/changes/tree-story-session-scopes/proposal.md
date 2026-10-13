## Why

A Claude Code session or a whole story can hold far more than the 500 descendants the chain read walks, one query per level (#1282). #1281 stamped every process with its story root and agent session and indexed both, so a whole scope is now a single equality lookup.

## What changes

- `GET /api/hosts/{id}/tree` accepts `scope=story&root=<process id>` and `scope=session&session=<id>`. Each returns every process stamped with that story or session on the host, read by its index with no time window, oldest first so a truncated page keeps creators ahead of their children, and built with the existing creator linking and sibling aggregation.
- The row limit, truncation flag and bounded count work as for the window read: one row past the limit proves truncation, and the count runs only then.
- A story scope without a positive `root`, or a session scope without a `session` of at most 256 bytes, is rejected with `invalid_scope`.

## Impact

- `server/detection/api/{service,types}.go`, `server/detection/internal/{graph,mysql,service,operator}`. The alert chain read and the timeline move onto these reads in later PRs of #1282.
