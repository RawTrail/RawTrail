## Why

A large event backlog stalled the detection processor (issue #1515). An agent offline for a few days uploads its queue on reconnect, and with 4.9M events queued for one host, choosing the next host to serve took 8 to 10 seconds and each claim of 500 events took 3 to 4. Every worker runs both on every tick, so the processor cleared about 1,500 events a minute against the agent's 2,000, the backlog grew, and the host's process tree and AI agent sessions fell days behind.

## What changes

- Choosing the next host reads one index entry per host with never-claimed work, not every queued event.
- A claim reads never-claimed and lease-expired events in two index-ordered reads that each stop at the batch size, and merges them oldest first, instead of one read that sorted the host's whole backlog.
- Which events are claimed, and in what order, is unchanged.

## Impact

- `server/visibility/internal/eventlog/store.go`. On a 3.1M-event queue, choosing a host fell from 0.41 s to 0.01 s and a claim from 0.56 s to under 10 ms.
