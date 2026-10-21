## 1. Reads

- [x] 1.1 Return the lineage fields from the tree, child, process-by-id and process-by-generation reads.
- [x] 1.2 Link the forest by creator row, then exact `(ppid, parent_pidversion)`, then `ppid`.
- [x] 1.3 Walk ancestors by creator row, and skip descendants linked to another row.
- [x] 1.4 Add the lineage fields to the UI's process type.

## 2. Tests

- [x] 2.1 Property-based test of the forest over exact edges, and table tests for each link rule.
- [x] 2.2 Integration tests for the pinned ancestor walk and the chain read under pid reuse and a double fork.
