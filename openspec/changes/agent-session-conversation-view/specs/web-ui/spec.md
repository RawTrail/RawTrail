## ADDED Requirements

### Requirement: Agent session conversation view

The agent session view's Timeline, grouped by prompt, SHALL list the session's turns, each a row headed by its prompt, labelled as a prompt and as reported by the agent, that opens to the commands and tool calls made for it in the order they happened; what belongs to no prompt comes last. A turn with an alert, and every turn a search lists, SHALL open on its own. It SHALL say when a prompt was not captured or is hidden, load turns a page at a time, and send its search to the server.

#### Scenario: Each prompt shows what the agent did for it

- **GIVEN** a session whose prompts led to commands and file tool calls
- **WHEN** the operator opens the Timeline grouped by prompt
- **THEN** each prompt is listed closed, labelled "Prompt", with how many tool calls and processes it led to, and what belongs to no prompt is listed last under "Outside any prompt"
- **AND** opening a prompt lists its commands and tool calls in the order they happened, each tool call labelled as reported by the agent

#### Scenario: A prompt the operator may not read is shown as hidden

- **GIVEN** an operator without `agent_prompt.read`
- **WHEN** they open the Timeline grouped by prompt
- **THEN** each prompt reads "Prompt hidden", and a turn whose prompt was not captured reads "Prompt not captured"
