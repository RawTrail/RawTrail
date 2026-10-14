## Why

The server totals what a process, its subtree or an AI agent session touched (`process-activity-rollup`, #1285), but nothing shows it. An operator asking "where did this tool call connect" or "what did this session touch" still reads the timeline event by event.

## What changes

- An activity view with Files, Network and DNS tabs: each tab's label counts its distinct paths, destinations or names, a line states its totals, and a table lists each entry with the process behind it and when it was first and last seen.
- It says when a list was cut, and when the scope reached only part of its processes (some without a generation, more than are read) or none of them.
- Process detail gains an Activity section with a "This process / Whole subtree" switch.
- The session view gains a "Tool calls / Activity" switch; a process named in the activity opens its detail beside the session.

## Impact

- `ui/src/{api,types}.ts`, `ui/src/components/Activity/`, `ui/src/components/ProcessDetail.tsx`, `ui/src/components/Sessions/SessionView.tsx`, and their tests.
- The fake agents send a flow's `pidversion` when a scenario sets it; the Claude Code scenario and journey gain git's lookup and connection.
