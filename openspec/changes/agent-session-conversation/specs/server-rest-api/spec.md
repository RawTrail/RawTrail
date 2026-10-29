## ADDED Requirements

### Requirement: Agent session conversation

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/conversation` SHALL list the session's turns oldest first, each with its prompt, the commands it ran (through the call each pairs with) and the calls that ran no command, with what belongs to no turn in a last turn without a prompt. It SHALL page by `cursor`, search by `q`, require process read, and include prompt text only with `agent_prompt.read`.

#### Scenario: Each prompt lists what the agent did for it

- **GIVEN** a session whose prompts each led to commands and file tool calls
- **WHEN** a client reads its conversation
- **THEN** each turn lists its prompt, the commands whose calls were made in it, and its other calls, each once

#### Scenario: What belongs to no prompt is kept, not dropped

- **GIVEN** a command no call pairs with, and a call read before any turn
- **WHEN** a client reads the conversation
- **THEN** both are listed in the last turn, which has no prompt

#### Scenario: Another owner's turn is a turn of its own

- **GIVEN** a prompt and a call another user's transcript wrote, naming the same turn as this session's
- **WHEN** a client reads the conversation
- **THEN** they form a turn of their own, apart from the session owner's, and take none of its commands

#### Scenario: Turns page in order

- **WHEN** a client follows `next_cursor` to the end
- **THEN** it reads every turn once, in order, the turn without a prompt last

#### Scenario: A search keeps the turns it finds

- **WHEN** a client asks with `q`
- **THEN** only the turns whose prompt text, a command, or a call's command or path holds it are listed

#### Scenario: Without prompt read the turns show without their text

- **GIVEN** a caller with process read but not `agent_prompt.read`
- **WHEN** they read the conversation, with or without a search
- **THEN** each turn is listed without its prompt's text, `prompts_hidden` is set, and a search does not match the text
