## Why

During a burst of process activity the macOS agent dropped most of the events its security extension delivered (#1584). Measured on edr-dev, an 11-second exec storm of about 144,000 events dropped about 120,000. The receiver's enqueue loop took only about 2,100 events per second. A CPU profile put almost all of the loop's time in SQLite's per-statement commit: the queue committed each event alone, which costs a WAL write and sync per event. The queue alone takes about 7,000 events per second that way on the VM's disk and 81,000 to 94,000 in batches of 100.

## What changes

- The receiver loop calls a new `OnDrained` hook once the events delivered so far have been handed to `OnEvent` and the channel is empty, or after 512 of them without a pause, and once more as the connection ends.
- The agent gathers each event's enriched bytes in `OnEvent` and writes them in `OnDrained` through `Coalescer.HandleBatch`, which buffers network and DNS events as before and writes everything else through the new `Queue.EnqueueBatch`, one transaction per batch with the cap enforced once.
- `Queue.Enqueue` is unchanged for its other callers. A batch is durable when its write returns and fails as a whole on a storage error, as one event does.

## Impact

- `agent/receiver/loop.go`, `agent/queue/queue.go`, `agent/coalesce/coalesce.go`, `agent/cmd/rawtrail-agent/{main,sensors,sensors_notwindows,sensors_windows}.go`.
- In steady traffic the channel is empty after nearly every event, so each is still written as it arrives.
