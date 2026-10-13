## 1. Schema

- [x] 1.1 Add the agent session, story root and tool call columns and their indexes.

## 2. Graph builder

- [x] 2.1 Stamp the session from an exec's markers and inherit it on every write path, in the overlay and the flush.

## 3. Tests

- [x] 3.1 Integration tests for a tool call, an env-stripped child, a daemonized descendant, a new session, a process outside any agent and an oversized marker.
- [x] 3.2 Property test: each process's session is its nearest marked ancestor's. The differential test compares the session.

## 4. Later PRs of #1281

- [ ] 4.1 Stamp `story_root_id` and `tool_call_group`.
- [ ] 4.2 Upsert the `agent_sessions` table.
