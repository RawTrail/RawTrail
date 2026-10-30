## Why

How much a session used is one of the first things people ask of it, and the session header answers it, but the sessions list did not: its rows come from MySQL and the tokens live in the event archive. Issue #1427.

## What changes

- `GET /api/agent-sessions` gives each row `tokens`: its input, cache and output tokens, counted as its summary counts them, read from the archive in one query over the page's sessions.
- The list is still served when the archive cannot be read, without the totals.
- The Agent sessions page lists each session's tokens.

## Impact

- `server/visibility` (a batched token read), `server/detection` (the list), `docs/api/openapi.yaml`, `ui/src/components/Sessions/SessionList.tsx`.
