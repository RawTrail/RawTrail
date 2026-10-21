## Why

The agent reports the tool calls an AI agent records in its transcript (`agent-tool-call-capture`, #1288), but the session view still labels each tool-call group only with the command its shell ran. Two identical commands look the same, and nothing names the call by its `tool_use_id` or shows the description the agent gave it.

## What changes

- `GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls` lists a session's tool calls from the event archive, oldest first and each once, however many times the agent reported it.
- The session view pairs each group an agent's wrapper shell started with its Bash call: same command, made before the group started, the nearest unclaimed one. A call the user refused is passed over by the group after it.
- Each paired group shows the call's description and `tool_use_id`. Identical calls of one turn share its time and cannot be told apart, so their groups are marked as one of several calls made at once, naming them, rather than paired by a guess.

## Impact

- `server/visibility` (archive read, in-memory fake), `server/detection/internal/operator` (handler), the OpenAPI spec.
- `ui/src/components/Sessions/` (the join, the view), `ui/src/{api,types}.ts`, and their tests; the fake agents send `agent_tool_call`.
