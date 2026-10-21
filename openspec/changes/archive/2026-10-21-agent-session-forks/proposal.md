# Link a forked agent session to the session it was forked from

## Why

Claude Code's `/fork` starts a session of its own that begins with a copy of its parent's history. Measured on edr-dev (Claude Code 2.1.300), the fork's transcript repeats the parent's earlier tool calls under the fork's session id, with their original times, so the fork's Commands listed calls it never made, and nothing said which session it came from (issue #1347).

Every line the fork copied keeps the `session_id` it was written under, and a copied call keeps its `tool_use_id`. That field is not by itself a fork marker: a process that switched sessions with `/clear` keeps writing its first session's id on the new session's own calls. A call is a copy only where the session it names holds the same call.

## What changes

- The agent sends a tool call's own `session_id` as `origin_session_id` when it differs from the session.
- The server lists a copied call under the session it came from only, and reads each session's parent and forks from the copies.
- A new endpoint, `GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage`, returns them.
- The session view links a fork to its parent and a session to its forks.

## Impact

- Wire: an optional `origin_session_id` on `agent_tool_call`.
- API: one new read endpoint.
- UI: one line under the session header.
- No storage change: the lineage is read from the event archive.
