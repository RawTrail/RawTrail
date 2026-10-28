## ADDED Requirements

### Requirement: Agent token usage reporting

The agent SHALL report the tokens each assistant message of an AI agent session used as one `agent_usage` event per message, whatever the prompt-capture setting: its model, input, output, cache-write, cache-read and thinking tokens, its turn, and the permission mode last put in force. It SHALL NOT count a message Claude Code wrote itself, or a line a forked session copied from its parent.

#### Scenario: A streamed message is counted once with its turn and mode

- **GIVEN** a message written as several transcript entries, after a prompt and a permission mode change
- **WHEN** the agent reads the transcript
- **THEN** it sends one `agent_usage` event for the message, naming the prompt's turn and the mode in force

#### Scenario: A message that used no tokens of this session is not counted

- **GIVEN** a `<synthetic>` message, and an entry a forked session copied from its parent
- **WHEN** the agent reads the transcript
- **THEN** it sends no `agent_usage` event for either
