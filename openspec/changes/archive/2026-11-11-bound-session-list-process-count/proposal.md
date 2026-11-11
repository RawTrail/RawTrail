## Why

After v0.9.0-rc.5 fixed the sessions list's timeout (#1544), the AI agent sessions page on the dogfood server still took 9 to 10 s (issue #1547). The list counts each listed session's processes, and a count reads the session's whole index range. A dogfood page holding a 762,084-process session read about 2.2M index entries and took 9.5 s. The laptop profile opens the console on this page, and the cost grows with every long session.

## What changes

- The sessions list counts each listed session's processes up to 10,000. A session past the bound reports 10,000 and `process_count_capped: true`, so a page costs the same however large its sessions are.
- The counts are one statement: one count per listed session, each reading at most 10,001 index entries.
- The Agent sessions page shows a capped count as `10,000+`, with "More than 10,000 processes" on hover.
- The session's own view still counts all of its processes. Alert counts are unchanged.

## Impact

- `server/detection/internal/mysql/agent_sessions.go`, `server/detection/api/types.go`, the OpenAPI spec, `ui/src/types.ts` and `ui/src/components/Sessions/SessionList.tsx`, with their tests.
- API: `GET /api/agent-sessions` rows gain `process_count_capped`, present only when true. `process_count` stops at 10,000.
