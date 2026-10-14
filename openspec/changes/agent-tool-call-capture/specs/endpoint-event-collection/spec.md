## ADDED Requirements

### Requirement: Agent tool call capture

The agent SHALL report each tool call an AI agent records in its session transcript as an `agent_tool_call` event, stamped at the transcript's time and carrying the session id, the `tool_use_id` and the tool name, a Bash call's command and description, and a file tool's path. It SHALL NOT send prompt text, model output or tool results. A transcript line it cannot read SHALL be skipped.

#### Scenario: A Bash tool call is reported with its command

- **GIVEN** a Claude Code session on the host
- **WHEN** the agent runs a Bash command
- **THEN** an `agent_tool_call` event carries the session id, the call's `tool_use_id`, `Bash`, and the command
- **AND** a command longer than 8 KiB is cut on a character boundary and marked `truncated`

#### Scenario: Prompts and results never leave the host

- **GIVEN** a transcript holding the user's prompts, the model's text and the tools' results beside its tool calls
- **WHEN** the agent reads it
- **THEN** only the tool calls produce events, and none carries a prompt, model text or a tool result

#### Scenario: An unreadable line is skipped, not guessed

- **GIVEN** a transcript line that is not JSON, or a tool call missing its session, id, name or time
- **WHEN** the agent reads it
- **THEN** no event is sent for that line and the reading continues with the next
