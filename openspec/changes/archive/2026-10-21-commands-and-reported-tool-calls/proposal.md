## Why

The session view's main lens was labelled **Tool calls**, but its rows are process trees the sensor observed, one per command the agent ran. The agent's own tool-call records come from its transcript, a file the agent and its user can write, and only partly overlap with those rows. Read, Edit and WebFetch calls start no process, while hooks and MCP servers have rows with no call. Labelling telemetry with the agent's vocabulary presents it as the agent's self-report (#1333).

## What changes

- The lens and its unit are named **Commands**: the toggle, the groups and the notices.
- A tool call shown beside a command, paired or ambiguous, is labelled "Reported by agent".
- The in-flight `web-ui` deltas that called a group a tool call now say command. Their scenario titles are unchanged, so test references keep resolving.

## Impact

- `ui/src/components/Sessions/` (view, tree shaping, tests), the agent-sessions Playwright journey, the CHANGELOG, and the `web-ui` deltas of `agent-session-view` and `agent-session-view-badges`.
