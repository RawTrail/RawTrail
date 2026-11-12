## Why

A long AI agent session's Activity tab took about 4 seconds on dogfood (v0.9.0-rc.6) before it showed anything (issue #1550). Its one response waited on the slowest of four ClickHouse reads (the totals, 2.7 s, while the lists took 1.6 to 2.1 s), it read the session's processes again although the session's other tabs had just read them, and 2.1 MB of its 2.2 MB was the names of the session's 20,000 processes, of which its rows name a few.

## What changes

- `GET /api/hosts/{host_id}/activity` takes `rows=false` to read the totals alone.
- The activity rollup and each page of `GET /api/hosts/{host_id}/activity/{class}` name only the processes their rows were matched by. A page now names its processes too.
- A session's activity reads share the session's kept process read with each other and with its other views.
- The Activity tab reads the totals and each class's first page at once, and shows each as it arrives. Until the operator picks a class, the first list to arrive with rows is shown.

## Impact

- `server/detection/internal/operator`: the activity handlers. `server/visibility`: the totals-only rollup.
- `ui/src/components/Activity`: the Activity tabs of the session view and of process detail.
- A client that named a row's process from the rollup's `processes` still can; a list read past the rollup names its processes in each page.
