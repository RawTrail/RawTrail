## Why

The server lists AI agent sessions (`agent-sessions-list-api`, #1283), but nothing in the UI shows them. An operator who wants to know what an agent did on a laptop has no place to start from the session itself: they have to know the host and the time first.

## What changes

- A "Sessions" page in the top navigation, gated on process read like the endpoint. It lists sessions most recently active first, with agent and version, project directory, host, first and last seen, process count, alert count and a short session id.
- The host, agent and "active in" filters live in the URL and are applied by the server, which also pages the list ("Load more").
- A session opens its host's page on a window ending at the session's last activity. The session view (#1284) replaces that target.
- The navigation order gains Sessions after Hosts.

## Impact

- `ui/src/{api,types,App}.ts(x)`, `ui/src/components/Sessions/`, `ui/src/components/ui/nav-links.ts`, and their tests.
- `test/e2e/tests/qa/agent-sessions.spec.ts`.
