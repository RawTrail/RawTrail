## Why

The dogfood server (v0.9.0-rc.5) read about 1 TB an hour from ClickHouse for alert evidence, and that load slowed every page that reads the event archive, a long AI agent session's tabs among them (issue #1550). Batches waiting for a process to be recorded are replayed, about twice a second on dogfood. Each replay evaluates its rules again and inserts each finding's alert again, and every insert read its triggering events from the archive. An id lookup reads some 550,000 rows, because the `event_id` bloom filter keeps about 2.5% of granules.

## What changes

- Before reading the archive, alert and monitor-record insertion take the envelope of any triggering event an alert already holds from `alert_event_payloads`, found through `alert_events`' `event_id` index. Only events no alert holds yet are read from the archive.
- A replayed finding therefore reads nothing from the archive. A new alert citing an event another alert holds copies that alert's envelope, which is the same envelope the archive would return.

## Impact

- `server/detection/internal/mysql/alerts.go` and `monitorrecords.go`, with a new test.
- No API or schema change. How often a batch is replayed is unchanged.
