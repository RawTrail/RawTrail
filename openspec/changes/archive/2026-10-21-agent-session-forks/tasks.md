# Tasks

- [x] Agent: send a tool call's `session_id` as `origin_session_id` when it differs from the session.
- [x] Server: leave a copied call out of the fork's tool calls; read the session lineage from the copies.
- [x] API: `GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage`, documented in OpenAPI.
- [x] UI: link a fork to its parent, and a session to its forks.
- [x] Tests: parser, payload round trip, archive (ClickHouse and in-memory), handler, UI.
- [x] Live QA: a `/fork` on edr-dev, replayed to the dev server.
