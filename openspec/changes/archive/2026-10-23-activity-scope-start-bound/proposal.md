## Why

The Activity tab on a busy host took about 3.6 s to load (dogfood, v0.8.0-rc.2). The rollup's four ClickHouse reads named the scope's process generations but no time, so each read the host's whole history of file, network and DNS events, and they ran one after another. Reading all of history also counted an older process that shared a generation: pidversion starts over at each boot, so a process from before a reboot can carry the generation of one in the scope.

## What changes

- The rollup reads only events at or after the scope's start: the earliest start or fork of its processes. A snapshot process with no recorded start, or a process with no time, leaves the read unbounded rather than cut short.
- The four reads run at once.

## Impact

- `server/visibility/api` (`ActivityFilter.FromNs`), `server/visibility/internal/clickhouse/activity.go`, `server/detection/internal/operator/activity_handler.go`.
