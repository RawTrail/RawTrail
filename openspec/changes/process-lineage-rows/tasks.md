## 1. Schema

- [x] 1.1 Add the lineage columns to `processes` in one instant migration.
- [x] 1.2 Add the fields to `api.Process` and every full-row write and read in the store.

## 2. Graph builder

- [x] 2.1 Decode the fork's `parent_pidversion` and the exec's lineage fields.
- [x] 2.2 Store them on each write path, mirrored in the batch overlay, and derive `reparented`.

## 3. Tests

- [x] 3.1 Property-based round trip for the exec and fork payloads with the lineage fields.
- [x] 3.2 Integration tests: each write path stores the lineage, the batch fold matches one-event batches, and a replay is a no-op.

## 4. Later PRs of #1280

- [x] 4.1 Resolve `parent_process_id` from the fork's exact `(parent_pid, parent_pidversion)`, and bind exits by `pidversion`.
- [x] 4.2 Build the tree and walk ancestors by `parent_process_id`.
