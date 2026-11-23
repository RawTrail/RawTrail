## 1. Detection

- [x] 1.1 Add `GetAgentSessionLedBy` to the rules' graph reader, the MySQL store, and the engine's retry and timing wrappers.
- [x] 1.2 Group a read by a process with no session marker under the session it leads.

## 2. Tests

- [x] 2.1 Rule test: the agent's own read shares the session's subject, and a process in no session keeps its own.
- [x] 2.2 Store integration test of the lookup's bounds: the process's lifetime, the read's moment, /clear, a recycled pid, another host.

## 3. Verify

- [x] 3.1 Replay the #1588 demo session on the dev server and confirm one `agent_credential_read` alert for its AWS reads.
