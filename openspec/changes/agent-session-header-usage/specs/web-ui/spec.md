## ADDED Requirements

### Requirement: Agent session header usage

The agent session header SHALL say what the session's messages used, from its totals: the models, the tokens, and each permission mode the agent ran in with how many replies and over how long. Bypass permissions SHALL be flagged. A session that recorded no message's usage SHALL show no usage line.

#### Scenario: The header says what the session's messages used

- **GIVEN** a session whose messages ran on one model, partly in bypass permissions mode
- **WHEN** the operator opens it
- **THEN** the header names the model, its tokens, and each mode with its replies and span, bypass permissions flagged
