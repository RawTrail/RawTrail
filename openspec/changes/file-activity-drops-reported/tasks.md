## 1. Extension

- [x] 1.1 Charge each dropped row's operations to the topmost process of its lineage
- [x] 1.2 Send one `file_activity_dropped` event per charged process per flush

## 2. Server and UI

- [x] 2.1 Count the scope's dropped operations as the digest's `unrecorded`
- [x] 2.2 Say on the session Summary, and note on its share card, how many file operations were not recorded

## 3. Tests

- [x] 3.1 A dropped row is charged to the root of its session; every drop is charged to a subtree (property)
- [x] 3.2 The digest counts only the scope's dropped operations
- [x] 3.3 Exercise on edr-dev with a window that still overflows
