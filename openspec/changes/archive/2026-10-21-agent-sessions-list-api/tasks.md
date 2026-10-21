## 1. API

- [x] 1.1 Store read: filters, keyset paging, and grouped process and alert counts.
- [x] 1.2 `GET /api/agent-sessions` handler, authorization and wiring.

## 2. Tests

- [x] 2.1 Store integration tests: counts, filters, keyset paging that never repeats a row, and an invalid cursor.
- [x] 2.2 Handler tests: parameters, window validation, cursor errors and authorization.

## 3. Later

- [ ] 3.1 The Sessions page in the UI (#1283).
