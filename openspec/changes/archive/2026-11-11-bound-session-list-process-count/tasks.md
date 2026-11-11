## 1. Server

- [x] 1.1 Count each listed session's processes up to the bound, in one statement, and report a capped count
- [x] 1.2 Document `process_count_capped` in the OpenAPI spec

## 2. UI

- [x] 2.1 Show a capped count as the bound followed by `+`, with the meaning on hover

## 3. Tests

- [x] 3.1 Counts on either side of the bound, through `ListAgentSessions`
- [x] 3.2 `EXPLAIN ANALYZE` pins that a count reads at most the bound and one more, mutation-checked against a count with no limit
- [x] 3.3 The list cell, capped and exact, mutation-checked
- [ ] 3.4 Open the Agent sessions page on dogfood after the RC is deployed
