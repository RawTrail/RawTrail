## Why

A stalled or slow detection pipeline was invisible in metrics (issue #1218). Alerts arrive late or not at all, and the time from an event arriving to detection running on it is the number a top EDR is judged on. The unused backlog-count method could not simply be wired to a gauge: an exact count of a 5.3M-row queue took 1.8 s, against the gauge's 2 s collection budget, so the gauge would go blank during exactly the backlog it exists to show.

## What changes

- `edr.detection.lag`: a histogram, in seconds, of each event's time from the server receiving it to its batch being acknowledged after detection. Server clock (`ingested_at_ns`), so an agent's offline buffer is not lag. Recorded only once the batch is acknowledged by the attempt that processed it.
- `edr.events.pending`: a gauge of queued events, counted up to 1,000,000 and reading the cap above that. `fleet-edr-server` reports it; `fleet-edr-ingest` does not.
- Two SigNoz alert rules: "EDR detection lag high" (p95 above 5 minutes for 10 minutes) and "EDR detection stalled" (events queued while no lag is recorded for 15 minutes).

## Not considered further

An oldest-pending-event age gauge: the cheap index is on the agent's clock, so offline uploads would read as lag, and an index on `ingested_at_ns` taxes every insert on the ingest path.
