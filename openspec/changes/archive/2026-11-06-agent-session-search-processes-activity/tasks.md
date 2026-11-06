## 1. Server

- [x] 1.1 Search a session's processes over its whole read, listed alone in fork order
- [x] 1.2 Page the search by cursor at `GET .../agent-sessions/{session_id}/processes`

## 2. UI

- [x] 2.1 Processes lists what the server found for a search, with Load more
- [x] 2.2 Activity reads each class's rows for a search from the server

## 3. Tests

- [x] 3.1 Search and paging units, a paging property, and the handler's pages and failures
- [x] 3.2 vitest for both searches, a Playwright journey over a multi-page session
