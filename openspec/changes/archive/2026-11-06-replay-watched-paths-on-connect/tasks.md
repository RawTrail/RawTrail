## 1. Agent

- [x] 1.1 `Dispatcher` holds the latest watched-path payload that found no connector and `Set` sends it to the connector it publishes.
- [x] 1.2 Tests: replay once, latest wins, a live push is not replayed, and a push racing the connection is neither lost nor duplicated.

## 2. QA

- [ ] 2.1 Live macOS VM: push the watched paths before the extension connects and confirm it holds them once it does.
