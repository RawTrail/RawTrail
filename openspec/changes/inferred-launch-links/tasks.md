## 1. Server

- [x] 1.1 `GetLaunchdChildren`: launchd's children in a fork-time range, on the `(host_id, ppid, fork_time_ns)` index.
- [x] 1.2 Infer launch links in tree reads; add linked processes and their descendants to session and story reads.

## 2. Tests

- [x] 2.1 Unit tests for launch-target parsing.
- [x] 2.2 Integration tests on the edr-dev capture's shape, for a session read and a window read.
