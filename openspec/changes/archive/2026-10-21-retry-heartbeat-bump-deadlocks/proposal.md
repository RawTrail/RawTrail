## Why

The heartbeat freshness bump runs at ingest, concurrently with other writes to the process table, and under load it regularly loses an InnoDB deadlock (issue #1243). Each loss logged an ERROR line, 181 of them in dev logs since the v0.7.0 release candidates, which buries real errors and would trip any alert on ERROR logs. The lost bump itself was harmless, because the next heartbeat re-applied it well inside the 6-hour TTL.

## What changes

- A heartbeat bump that loses a deadlock is retried a few times with a short backoff, as the event log and rule statistics writers already are. The deadlock rolls the bump back whole, so the retry applies it once.
- A bump that still fails after the retries is logged at ERROR as before, because that is no longer ordinary contention.

## Not changed

The freshness scoping, its no-op cases, and the batch's acceptance are unchanged. A failed bump still never fails the ingest request.
