## ADDED Requirements

### Requirement: Agent session conversation view

The agent session view SHALL offer a Conversation view listing the session's turns from the server: each prompt, labelled as reported by the agent, with the commands and tool calls the agent made for it, and last what belongs to no prompt. It SHALL say when a prompt was not captured or is hidden from the operator, load turns a page at a time, and send its search to the server.

#### Scenario: Each prompt shows what the agent did for it

- **GIVEN** a session whose prompts led to commands and file tool calls
- **WHEN** the operator opens the Conversation view
- **THEN** each prompt is listed with its commands and calls beneath it, and what belongs to no prompt is listed last under "Outside any prompt"

#### Scenario: A prompt the operator may not read is shown as hidden

- **GIVEN** an operator without `agent_prompt.read`
- **WHEN** they open the Conversation view
- **THEN** each prompt reads "Prompt hidden", and a turn whose prompt was not captured reads "Prompt not captured"
