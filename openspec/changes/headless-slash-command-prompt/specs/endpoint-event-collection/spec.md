## MODIFIED Requirements

### Requirement: Agent prompt capture

Unless prompt capture is off, the agent SHALL report each prompt a person typed to an AI agent or passed it on the command line, as its transcript records it, as an `agent_prompt` event stamped at the transcript's time, carrying the session id, the turn, the text with each recognizable credential replaced and cut to 16 KiB, the length as typed and the owner's uid. A turn the agent or its tooling wrote SHALL NOT be reported. A prompt naming a session not its transcript's SHALL be skipped.

#### Scenario: A typed prompt is reported with its turn

- **GIVEN** a person types a prompt to Claude Code, which then runs a Bash command for it
- **WHEN** the agent reads the transcript
- **THEN** an `agent_prompt` event carries the session id, the prompt's text and its turn, stamped with the transcript owner's uid
- **AND** the `agent_tool_call` of the command names the same turn
- **AND** a slash command the person typed reads as `/name args`

#### Scenario: Only what a person typed is a prompt

- **GIVEN** a transcript holding a skill's instructions the agent wrote, a background task's notification, a compacted history, a subagent's instructions, a tool's result, and a turn that records neither an origin nor a prompt source, outside a slash command passed on the command line
- **WHEN** the agent reads it
- **THEN** none of them is reported as a prompt

#### Scenario: A prompt passed on the command line is reported

- **GIVEN** a person runs `claude -p` with a prompt, which the transcript records with no origin and the prompt source `sdk`
- **WHEN** the agent reads the transcript
- **THEN** an `agent_prompt` event carries the prompt's text, with `sdk` as its source

#### Scenario: A prompt is redacted and bounded before it leaves

- **GIVEN** a prompt holding a cloud access key, and a prompt longer than 16 KiB
- **WHEN** the agent reads them
- **THEN** the key is replaced by a placeholder naming its kind, and the event counts one credential replaced
- **AND** the long prompt is cut on a character boundary, marked cut, and carries its length as typed

#### Scenario: A slash command passed on the command line is reported

- **GIVEN** a person runs `claude -p` with a slash command, which Claude Code records as a local command: a meta caveat entry, then the command as plain text with neither an origin nor a prompt source
- **WHEN** the agent reads the transcript
- **THEN** an `agent_prompt` event carries the command as `/name args`, with its turn and `sdk` as its source
- **AND** the same text not following that caveat, a line the agent wrote, or an interactive local command such as `/clear` is not reported
