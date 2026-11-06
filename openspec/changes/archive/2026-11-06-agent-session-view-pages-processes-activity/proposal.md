## Why

The session view's Processes and Activity showed only the first page the server read: 5000 processes, 200 rows of each activity class, with a notice that more existed and no way to see them (#1425). The server now pages both by cursor (#1424).

## What changes

- Processes offers the session's next processes on request, each listed under the process that created it when an earlier page read that process.
- Each Activity class (files, network, DNS) offers its next rows on request, from the cursor the rollup or the last page named, until none remain.
- A failed read of either says so and offers the page again.

## Impact

- `ui/src/components/Sessions/SessionView.tsx`, `treePages.ts`, `ui/src/components/Activity/ActivityTabs.tsx`, `ui/src/api.ts`, `ui/src/types.ts`.
