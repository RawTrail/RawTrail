## Why

The session view counts everything it shows (processes, alerts, activity) from the rows it loaded, after reading up to 5000 processes in one go (#1422). Once Commands, Processes and Activity are paged (#1423, #1424, #1425), counts taken from loaded rows describe a page, not the session. The Summary tab (#1429) needs the same totals.

## What changes

- New `GET /api/hosts/{host_id}/agent-sessions/{session_id}/summary`: a session's retained processes, its alerts by severity, its tool calls by tool, and its file events by operation with its network destination and DNS name totals, counted by the server over the whole session.
- Activity is counted over the scope the session's activity rollup reads, and the response reports how far that scope reached, as the rollup does.

## Impact

- `server/detection/internal/operator`: the summary handler; the activity handler's scope resolution is shared with it.
- `server/detection/internal/mysql`: process and alert-by-severity counts for one session.
- `server/visibility`: `EventArchive.ActivityTotalsForGenerations` and `EventArchive.AgentToolCallCounts`.
- Not changed: the activity rollup's own response, the sessions list, the UI (which adopts the endpoint in #1425 and #1429). Covering sessions whose processes exceed the rollup's generation cap is #1424.
