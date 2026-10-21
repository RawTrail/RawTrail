## 1. Graph builder

- [x] 1.1 Resolve the creator row on fork and on an exec with no fork seen, in the store and the batch overlay.
- [x] 1.2 Link a creator created in the same batch after the batched insert, and in the per-row fallback.
- [x] 1.3 Bind exits by `pidversion`, including the pending-exit buffer.

## 2. Tests

- [x] 2.1 Integration tests for pid reuse, a parent's first exec, a dropped exec, the double fork, legacy events, exit binding and replay.
- [x] 2.2 Extend the differential property test with generations and compare the creator link.

## 3. Later

- [x] 3.1 Build the tree and walk ancestors by `parent_process_id` (#1280, next PR).
