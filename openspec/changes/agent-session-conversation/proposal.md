## Why

A session reads as a list of commands, but what people want is "you asked X, the agent did Y". The prompts (#1426) and each tool call's turn are now on the server, so the session can be read as its turns. Issue #1322, server half.

## What changes

- New `GET /api/hosts/{host_id}/agent-sessions/{session_id}/conversation`: the session's turns, oldest first. Each turn has the prompt that began it, the commands the agent ran for it (joined through the tool call each pairs with), and the tool calls that ran no command (file and web tools, MCP).
- Commands and calls that belong to no turn are listed in a final turn with no prompt, never dropped.
- Turns are paged by cursor, and kept by `q` over prompt text, commands, and a call's command or path.
- The route is gated on process read for the host. A prompt's text is included only for a caller that also holds `agent_prompt.read`; without it the turns still show, `prompts_hidden` is set, and a search never reaches the text.
- The visibility tool-call read now carries each call's `prompt_id`, which the agent already sends.

## Impact

- `server/detection/internal/sessioncommands`, `server/detection/internal/operator`, `server/visibility` (the tool-call read), `docs/api/openapi.yaml`.
- The Conversation tab follows under #1322.
