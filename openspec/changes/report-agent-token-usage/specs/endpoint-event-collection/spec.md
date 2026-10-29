## ADDED Requirements

### Requirement: Agent token usage reporting

The agent SHALL report the tokens each assistant message of an AI agent session used as one `agent_usage` event per message, whatever the prompt-capture setting: its model, input, output, cache-write, cache-read and thinking tokens, its turn, and the permission mode last put in force. It SHALL NOT count a message Claude Code wrote itself, and SHALL send a line naming another session with that session named, for the server to tell a fork's copy from a message of a session /clear started.

#### Scenario: A streamed message is counted once with its turn and mode

- **GIVEN** a message written as several transcript entries, after a prompt and a permission mode change
- **WHEN** the agent reads the transcript
- **THEN** it sends one `agent_usage` event for the message, naming the prompt's turn and the mode in force

#### Scenario: A message Claude Code wrote itself is not counted

- **GIVEN** a `<synthetic>` message
- **WHEN** the agent reads the transcript
- **THEN** it sends no `agent_usage` event for it

#### Scenario: A line naming another session is sent naming it

- **GIVEN** an assistant entry whose own `session_id` names another session, as a fork's copy or a message after `/clear` does
- **WHEN** the agent reads the transcript
- **THEN** it sends the usage with `origin_session_id` naming that session
