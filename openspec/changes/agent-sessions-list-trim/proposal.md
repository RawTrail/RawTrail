## Why

QA of the Agent sessions page found its filters added little: Host matched only a raw host id while the table shows host names, and none of the filters narrowed the prompt search above the list. Its first and last seen columns wrapped, leaving AM or PM alone on a second line, and said when a session started rather than how long it ran.

## What changes

- The Host, Agent, Active in and Permissions filters are removed from the page. The list is every session, most recently active first, paged by the server. The bypass badge stays on each row.
- First seen and Last seen become Last active, as a relative time ("3d ago"), and Duration ("2h 14m"), each on one line with the exact times on hover.

## Impact

- `ui/src/components/Sessions/SessionList.tsx`, `ui/src/components/Search/SearchPage.scss`, their tests and two Playwright journeys.
- The `/api/agent-sessions` filter parameters are unchanged.
