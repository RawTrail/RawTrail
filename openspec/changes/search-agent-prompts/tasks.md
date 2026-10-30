## 1. Server

- [x] 1.1 The prompt_text column and its ngram skip index
- [x] 1.2 The archive's search, newest first, keyset-paged, in a window, optionally of one host
- [x] 1.3 `GET /api/agent-prompts`, bounded, behind `agent_prompt.read` and audited with its query

## 2. Tests

- [x] 2.1 ClickHouse: across hosts, case, window, host, paging, LIKE escaping, and the skip index read
- [x] 2.2 Handler: the search asked, its audit resource, the bounds and the failures
