## 1. Owner-only usage

- [x] 1.1 Header totals and list tokens count only the owner's lines
- [x] 1.2 The bypass record keeps the uid, and the list matches it against the owner

## 2. Tests

- [x] 2.1 ClickHouse, list and summary tests that another uid's line changes nothing, and a session with no owner reports none
