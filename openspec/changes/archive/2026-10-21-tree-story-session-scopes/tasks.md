## 1. Reads

- [x] 1.1 Store reads and bounded count by `(host_id, story_root_id)` and `(host_id, agent_session_id)`.
- [x] 1.2 `BuildScopedTree` and the `scope=story` / `scope=session` handler parameters.

## 2. Tests

- [x] 2.1 Handler tests: routing, parameters, invalid scopes.
- [x] 2.2 Integration tests: a session larger than the chain walk's cap, truncation, cross-host isolation, the indexed read against the walk, and a fixed query count.

## 3. Later PRs of #1282

- [x] 3.1 The alert chain read uses the story index.
- [ ] 3.2 The timeline scopes by session or story server-side.
