## Why

The server read at most 5000 of a session's processes for anything it computed about the session: its commands, its summary, and its activity rollup (#1424). A long autonomous session runs past that, and everything past it was left out, reported only as a truncation. Reading more than 5000 in one query holds the whole read at once, and naming every generation of a large session in one ClickHouse filter overran ClickHouse's default query size at about ten thousand generations.

## What changes

- A session's commands, its summary and its activity rollup are computed over up to 20,000 of its processes, read a chunk of 5000 at a time in fork order. A session larger than that still reports that its read was cut.
- The session and story tree reads page by an opaque `cursor`: a cut page names the next, each page reports the scope's total, and a session's agents lead its first page only. A client still reads at most 5000 rows a request.
- A story's activity, and the timeline's scopes, keep the chain's 5000 bound.
- The activity rollup names a cursor for each class list it cut, and `GET /api/hosts/{host_id}/activity/{class}` reads that class's next page.
- Counting a scoped read's total reaches 100,000, past the largest read, so a cut read still reports its real total.

## Impact

- `server/detection/internal/mysql`: `GetScopedProcesses` reads from a position; `CountScopedProcesses` counts further.
- `server/detection/internal/graph`: `BuildScopedTree` reads in chunks up to `MaxScopedRead`.
- `server/detection/internal/operator`: the commands, summary and session activity reads use the larger bound; the tree read takes and hands out a cursor.
- `server/visibility/internal/clickhouse`: activity reads raise their query size; each class's list orders by its whole key and pages from a cursor.
