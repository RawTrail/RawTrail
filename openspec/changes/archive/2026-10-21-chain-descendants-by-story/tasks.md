## 1. Read

- [x] 1.1 Read a chain's candidates by the story index and walk them in memory, falling back to the per-process walk.

## 2. Tests

- [x] 2.1 Integration tests: the story read matches the walk (with an unstamped child, a reused pid and an unrelated process), and a chain past the walk's cap is read whole in a fixed number of queries.
