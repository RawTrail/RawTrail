## Why

The session view pages its processes and activity from the server (#1425), but its search did not follow: the Processes search looked only through the pages already loaded, so a process on a page not yet read was not found, and Activity had no search at all. Issue #1452.

## What changes

- New `GET /api/hosts/{host_id}/agent-sessions/{session_id}/processes?q=`: the session's processes whose path or command line holds the query, over the whole session read, each listed alone in fork order and paged by cursor.
- The session view's search, in Processes, lists what the server found, with "Load more", instead of searching the loaded tree.
- In Activity, the same search reads each class's rows from the server with `q`, and the session's totals stay its own.

## Impact

- `server/detection/internal/sessioncommands`, `server/detection/internal/operator`, `docs/api/openapi.yaml`.
- `ui/src/components/Sessions/SessionView.tsx`, `ui/src/components/Activity/ActivityTabs.tsx`, `ui/src/api.ts`.
