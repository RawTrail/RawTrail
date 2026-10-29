## Why

The session summary now totals what a session's messages used (#1457), but the session view did not show it. The two questions people ask first of an agent session are how much it took and whether it ran with permissions bypassed. Issue #1427.

## What changes

- The session header gains a line with the session's models, its tokens (a compact total, with input, cache and output in the tooltip), and each permission mode the agent ran in with how many replies and over how long.
- Bypass permissions is flagged with a badge.
- Known modes read by name, a mode the agent adds later reads as recorded, and messages written before any mode was named read "Mode not recorded".
- No cost estimate yet: where the per-model price table lives is still open in #1427.

## Impact

- `ui/src/components/Sessions/SessionView.tsx`, a new `sessionUsage.ts`, `ui/src/types.ts`, styles.
