## Why

The transcript the agent already reads records the tokens each assistant message used and the permission mode in force, but none of it reaches the server. "How much did it cost" and "did it run with permissions bypassed" are the first two questions people ask of a session. Issue #1427, agent half.

## What changes

- The agent sends one `agent_usage` event per assistant message: its model, its input, output, cache-write, cache-read and thinking tokens, the turn it was made in, and the permission mode the transcript last put in force.
- A streamed message is written as several transcript entries, each carrying the whole message's usage, so the event id is derived from the message id and the archive keeps one row per message.
- A message Claude Code wrote itself (`<synthetic>`) is not counted. A line naming another session is sent naming it (`origin_session_id`, and the fork parent its job names), since a fork's copy and a message after `/clear` look alike; the server's totals leave out a message the named session also holds, as they do for tool calls.
- Usage is metadata, not content, so it is sent whatever the prompt-capture setting.

## Impact

- `agent/agenttranscript`, `schema/events.json`, the event-type label set, `docs/api/openapi.yaml`.
- The server's totals, the sessions list filter and the session header follow in later changes under #1427.
