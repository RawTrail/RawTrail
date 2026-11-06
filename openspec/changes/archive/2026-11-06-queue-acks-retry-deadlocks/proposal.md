## Why

Acknowledging or returning a processed batch did not retry a MySQL deadlock, unlike claiming, appending and pruning (issue #1522). The acknowledgement meets the prune sweep on the queue's gap locks, and one deadlock left the batch in flight for its whole 5-minute claim lease: the host's later events waited behind it, and the batch was then processed a second time. On a laptop install there is one host, so each deadlock paused all of the AI agent's processing for 5 minutes. Draining a backlog on the dev server, it happened twice in 45 minutes.

## What changes

- Ack and Nack retry a deadlock with the bounded backoff the other queue writes use. MySQL rolls back the whole transaction on a deadlock, so running it again is safe.

## Impact

- `server/visibility/internal/eventlog/store.go`.
