## Why

Nothing on the sessions list said which sessions ran with permissions bypassed. Issue #1427.

## What changes

- A bypassed session's row carries a "Bypass permissions" badge, whose title says a fork of a bypassed session carries it too.
- The fake agent sends `agent_usage`, so the journey runs on a real ingest.

## Impact

- `ui/src/components/Sessions/SessionList.tsx`, `ui/src/api.ts`, `ui/src/types.ts`, `test/fakeagent`, a Playwright journey.
