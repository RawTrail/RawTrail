## REMOVED Requirements

### Requirement: Agent tool call capture

**Reason**: Its rule that prompt text never leaves the host is reversed by prompt capture (#1426), and a MODIFIED block cannot drop the scenario that stated it.

**Migration**: Replaced by "Agent tool call reporting", which keeps the other three scenarios word for word, states that model output and tool results never leave the host, and adds the turn a call was made in.

## ADDED Requirements

### Requirement: Agent tool call reporting

The agent SHALL report each tool call an AI agent records in its session transcript as an `agent_tool_call` event, stamped at the transcript's time, carrying the session id, `tool_use_id`, tool name, a Bash call's command and description, a file tool's path, and its turn when one precedes it. It SHALL NOT send model output or tool results. A line it cannot read, or naming a session not its transcript's, SHALL be skipped; each call SHALL carry its transcript owner's uid.

#### Scenario: A Bash tool call is reported with its command

- **GIVEN** a Claude Code session on the host
- **WHEN** the agent runs a Bash command
- **THEN** an `agent_tool_call` event carries the session id, the call's `tool_use_id`, `Bash`, and the command
- **AND** a command longer than 8 KiB is cut on a character boundary and marked `truncated`

#### Scenario: Model output and tool results never leave the host

- **GIVEN** a transcript holding the model's text and the tools' results beside its tool calls
- **WHEN** the agent reads it
- **THEN** no event carries the model's text or a tool's result

#### Scenario: An unreadable line is skipped, not guessed

- **GIVEN** a transcript line that is not JSON, or a tool call missing its session, id, name or time
- **WHEN** the agent reads it
- **THEN** no event is sent for that line and the reading continues with the next

#### Scenario: A forged line cannot stand in for another session's call

- **GIVEN** a user writes into their own transcript a line carrying another session's id and one of its call ids
- **WHEN** the agent reads it
- **THEN** no event is sent for that line
- **AND** a call is stamped with its transcript owner's uid, which its event id is derived from, so another user's line can never take its event id

### Requirement: Agent prompt capture

Unless prompt capture is off, the agent SHALL report each prompt a person typed to an AI agent, as its transcript records it, as an `agent_prompt` event stamped at the transcript's time, carrying the session id, the turn, the text with each recognizable credential replaced and cut to 16 KiB, the length as typed and the owner's uid. A turn the agent or its tooling wrote SHALL NOT be reported. A prompt naming a session not its transcript's SHALL be skipped.

#### Scenario: A typed prompt is reported with its turn

- **GIVEN** a person types a prompt to Claude Code, which then runs a Bash command for it
- **WHEN** the agent reads the transcript
- **THEN** an `agent_prompt` event carries the session id, the prompt's text and its turn, stamped with the transcript owner's uid
- **AND** the `agent_tool_call` of the command names the same turn
- **AND** a slash command the person typed reads as `/name args`

#### Scenario: Only what a person typed is a prompt

- **GIVEN** a transcript holding a skill's instructions the agent wrote, a background task's notification, a compacted history, a subagent's instructions, a tool's result, and a turn from an agent that records no origin
- **WHEN** the agent reads it
- **THEN** none of them is reported as a prompt

#### Scenario: A prompt is redacted and bounded before it leaves

- **GIVEN** a prompt holding a cloud access key, and a prompt longer than 16 KiB
- **WHEN** the agent reads them
- **THEN** the key is replaced by a placeholder naming its kind, and the event counts one credential replaced
- **AND** the long prompt is cut on a character boundary, marked cut, and carries its length as typed
