## 1. Server

- [x] 1.1 `ActivityForGenerations` on the event archive: totals and capped per-process lists of file, network and DNS activity.
- [x] 1.2 `GET /api/hosts/{host_id}/activity` resolving a process, subtree, session or story scope.

## 2. Tests

- [x] 2.1 ClickHouse integration test against hand-counted totals, including a coalesced flow, a reused pid, a legacy event and a cut list.
- [x] 2.2 Handler tests for each scope, an unknown process, and the error paths.
