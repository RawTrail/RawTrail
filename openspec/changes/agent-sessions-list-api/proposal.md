## Why

The epic's goal is a place to see what AI agents did (#1275). #1281 records one row per agent session, but nothing lists them (#1283).

## What changes

- `GET /api/agent-sessions` lists AI agent sessions across the fleet, most recently active first. Each row carries the agent, version, session id, host, the project directory (the earliest tool call's cwd), first and last seen, and the counts of its processes and alerts (not monitor records) still retained. The session row outlives retention, so an old session's counts shrink as its rows are pruned.
- It filters by `host_id`, `agent` and a `from`/`to` window the session was active in, and pages with an opaque keyset cursor, following the fleet search contract, with the filtered total.
- It is gated on fleet-wide process read, like the process search.

## Impact

- `server/detection/api/types.go`, `server/detection/internal/{mysql,operator}`, `server/detection/bootstrap`, the API doc and the OpenAPI spec. The UI page follows in the next PR.
