## Why

The server now serves a session as its turns (#1456), but the session view reads only as commands, processes and activity. Issue #1322, UI half.

## What changes

- A Conversation view, first among the session's views. It lists each prompt the person typed, labelled as the agent's report, with the commands the agent ran for it and the tool calls that ran none (file and web tools, MCP). What belongs to no prompt comes last, under "Outside any prompt".
- A turn whose prompt was not captured says so. Without `agent_prompt.read`, each prompt reads "Prompt hidden".
- Turns load a page at a time. The view's search asks the server over prompt text, commands and calls, and the view says when a session read was cut.
- Commands stays the view a session opens on.

## Impact

- `ui/src/components/Sessions/SessionView.tsx` and its styles, `ui/src/api.ts`, `ui/src/types.ts`, an end-to-end journey.
