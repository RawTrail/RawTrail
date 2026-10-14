## 1. Server

- [x] 1.1 Archive read of a session's tool calls, one per `tool_use_id`, in ClickHouse and the in-memory fake.
- [x] 1.2 The tool-calls endpoint.

## 2. UI

- [x] 2.1 The join of groups to calls, with ambiguity for calls of one turn.
- [x] 2.2 The session view's call line.

## 3. Tests

- [x] 3.1 ClickHouse integration, fake and handler tests.
- [x] 3.2 vitest, including a property test of the join.
- [x] 3.3 Playwright: the Claude Code session's calls name their `tool_use_id`.
