## Why

Two routine events were logged as faults. Both server binaries closed the MySQL pool before the final OTel flush on a graceful stop. In `fleet-edr-server`, which registers them, the three gauges that count rows in MySQL logged a WARN each and lost their last export (issue #1274). And an agent that disconnected mid-upload, as on an agent restart, upgrade or sleep, made the ingest handler log an ERROR for the cancelled write (issue #1291). Both bury real errors and would trip any alert on the log level.

## What changes

- A server's stop cancels its context, flushes OTel, and only then closes the MySQL pool, so the final collection of every gauge still reaches the database.
- An ingest write that fails because the agent's request was cancelled, including the best-effort heartbeat bump and host upsert, is logged at INFO and gets no response, since nothing is left to read it. The batch was not acknowledged, so the agent resends it.
- Any other ingest write failure, including a cancellation while the request is still live, stays an ERROR. A failed archive or queue write is still answered with a 5xx; the best-effort writes still acknowledge the batch.

## Not changed

What a failed ingest write does to the batch is unchanged: it is not acknowledged and the agent retries it, and both stores stay idempotent by `event_id`.
