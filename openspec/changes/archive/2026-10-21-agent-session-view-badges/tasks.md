## 1. Server

- [x] 1.1 Filter `GET /api/alerts` by `agent_session` with `host_id`, rejecting it without the host.
- [x] 1.2 Filter `GET /api/agent-sessions` by `session`.

## 2. UI

- [x] 2.1 Read the session's record and alerts with its tree; header from the record and the retention notice.
- [x] 2.2 Alert badges on processes and tool calls, privilege badges, alerted processes kept out of the plumbing.
- [x] 2.3 Expand only the alerted calls of a long session.

## 3. Tests

- [x] 3.1 Store and handler tests for both filters.
- [x] 3.2 vitest for the grouping (kept processes, privilege changes) and the page.
