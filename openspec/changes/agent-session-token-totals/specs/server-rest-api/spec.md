## ADDED Requirements

### Requirement: Agent session token totals

The agent session summary SHALL carry `usage`: the session's input, output, cache-write, cache-read and thinking tokens and messages by model, and its messages by permission mode with the first and last of each. Each message SHALL be counted once by its id and owner, and a message the session it names also holds, a fork's copy, SHALL NOT be counted.

#### Scenario: A session's tokens are totalled by model and its messages by mode

- **GIVEN** a session whose messages were written by two models, in two permission modes
- **WHEN** a client reads its summary
- **THEN** `usage` totals each model's tokens and counts each mode's messages, with the span each mode was seen over

#### Scenario: A message is counted once, and a copy not at all

- **GIVEN** a message the agent sent twice, and a message a fork copied from the session it names
- **WHEN** a client reads the fork's summary
- **THEN** the first counts once and the copy not at all
