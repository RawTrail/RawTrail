## Why

Whether an agent session ran with permissions bypassed is one of the first things people ask of it, but the sessions list could not be filtered on it: the list reads MySQL, and permission modes lived only in the event archive. Issue #1427.

## What changes

- The graph builder records each agent session that an `agent_usage` event names in the `bypassPermissions` mode, at its earliest such event.
- A forked session carries copies of its parent's lines, so a fork of a bypassed session is recorded too. The session header leaves copies out, so a fork that never ran bypassed itself is listed as bypassed but shows no bypassed time in its header; the filter is labelled "Ran in bypass mode (or forked from a session that did)".
- `GET /api/agent-sessions` takes `bypass=true` and each row carries `bypass`.

## Impact

- `server/detection` (migration `00024`, the graph builder, the sessions list), `docs/api/openapi.yaml`.
