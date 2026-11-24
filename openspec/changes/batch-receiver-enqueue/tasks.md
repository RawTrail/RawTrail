## 1. Agent

- [x] 1.1 `Queue.EnqueueBatch`: one transaction per batch, the cap enforced once, all or nothing.
- [x] 1.2 `Coalescer.HandleBatch` and `WithBatchSink`: network and DNS events buffered as before, the rest written together.
- [x] 1.3 The receiver loop's `OnDrained` hook, and the agent gathering each drain and writing it there.

## 2. Tests

- [x] 2.1 A burst is written in batches of at most 512, the last as soon as the channel empties; what was gathered is written when the connection ends.
- [x] 2.2 Each drain is written once; the batch sink gets the passed events in order; a batch is ordered, readable on return, fails whole on a full disk, respects the cap, and is refused by a closed queue.
- [x] 2.3 A stop mid-burst or at its end still writes what was gathered, with a context the stop does not cancel, and shutdown waits for the receiver loops, bounded, before it flushes and closes the queue.

## 3. Verify

- [x] 3.1 Profile the enqueue loop under an exec storm on edr-dev, then rerun the storm with batching and count drops.
