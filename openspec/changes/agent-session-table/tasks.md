## 1. Schema and writes

- [x] 1.1 Add the `agent_sessions` table.
- [x] 1.2 Record a sighting per marked exec in the overlay and upsert it at flush on both flush paths.

## 2. Tests

- [x] 2.1 Integration tests: one row per session from its earliest tool call, out-of-order batches, replay, and batched versus one event per batch.
