## Why

An operator investigating what a process, its subtree, or an AI agent session did has to read the timeline event by event to learn which files it touched, where it connected, and what it resolved. Epic #1275 asks for those as totals and lists for any process, subtree or session (#1285).

## What changes

- `GET /api/hosts/{host_id}/activity?scope=process|subtree|session|story` returns, per class (files, network, DNS), the event total, the distinct artifacts, and a capped list grouped by artifact and by the process that touched it, with the scope's processes so a client can name them.
- **Join by process key, not ingest stamping.** The scope's processes are resolved with the process tree's indexed reads (#1282), and the archive is filtered by exact `(pid, pidversion)`, the rule the alert-chain timeline uses. Events are archived at ingest, before the graph builder assigns a process its session or story; stamping them would mean rewriting archive rows after the fact, and a re-derived session would leave them stale.
- A coalesced connection or query counts as the occurrences it stands for. An event without a pidversion, or one of a later process that reused the pid, is not counted. The same 5,000-generation bound and scope report as the timeline apply.

## Impact

- `server/visibility/{api,internal/clickhouse}`, `server/detection/{api,internal/mysql,internal/operator,bootstrap}`, `docs/api/openapi.yaml`, `docs/api.md`.
