## Why

An AI agent session's Summary and Activity tabs are slow for a long session (issue #1550). On the dogfood server (v0.9.0-rc.5), a session of 160,709 processes took 12.4 s to show its Summary and 4.8 s its Activity. Each read of the session's activity named the session's first 20,000 process generations as `(pid, pidversion)` tuples in the query text, about 330 KB of SQL that ClickHouse parsed on every read, and a summary runs several such reads at once.

## What changes

- An activity read sends its scope's generations to ClickHouse as an external table beside the query, not in the query's text. The query's size no longer grows with the scope, and the 8 MiB query size the reads raised is no longer needed.
- The read matches an event's pid against the table before parsing its payload for the pidversion, so only the scope's events are parsed.
- What a read returns is unchanged. Generations bounded to a span (an agent process that served several sessions) are few and stay in the query text.

## Impact

- `server/visibility/internal/clickhouse/activity.go`, `activity_page.go` and `digest.go`: the Summary's digest and totals, the Activity tab's rollup and pages, and the session header's totals.
- On dogfood, the summary's window read takes 0.25 to 0.3 s this way against 0.8 to 1.0 s with the tuples in its text.
