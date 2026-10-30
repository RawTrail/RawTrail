## Why

The sessions list can be filtered by the server to the sessions that ran with permissions bypassed (#1477), but the page offered no way to ask, and nothing on a row said so. Issue #1427.

## What changes

- A Permissions filter on the Agent sessions page, "Ran in bypass mode (or forked from a session that did)", applied by the server.
- A bypassed session's row carries a "Bypass permissions" badge, whose title says a fork of a bypassed session carries it too.
- The fake agent sends `agent_usage`, so the journey runs on a real ingest.

## Impact

- `ui/src/components/Sessions/SessionList.tsx`, `ui/src/api.ts`, `ui/src/types.ts`, `test/fakeagent`, a Playwright journey.
