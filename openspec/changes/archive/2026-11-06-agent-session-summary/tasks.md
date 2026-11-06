## 1. Server

- [x] 1.1 `EventArchive.ActivityTotalsForGenerations` and `EventArchive.AgentToolCallCounts` in the ClickHouse store, mirrored in the in-memory archive where it can be.
- [x] 1.2 `Store.AgentSessionCounts`: retained processes and alerts by severity for one session.
- [x] 1.3 The summary route, sharing the activity handler's scope resolution and the tool-call read's window.
- [x] 1.4 Tests: integration against real ClickHouse and MySQL, handler tests for the response, 404, 503 and errors.
