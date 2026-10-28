## 1. Server

- [x] 1.1 A scoped read from a position, chunked up to 20,000 rows, with the count reaching past it.
- [x] 1.2 Session commands, summary and activity read the whole session up to the bound.
- [x] 1.3 Activity reads send a query size large enough for a whole session's generations.
- [x] 1.4 Tests: a 12,346-process session read whole against real MySQL, a 20,000-generation activity read against real ClickHouse.
- [x] 1.5 The session and story tree reads page by cursor, agents on the first page only.
