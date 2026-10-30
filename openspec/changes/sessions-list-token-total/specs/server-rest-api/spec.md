## ADDED Requirements

### Requirement: Sessions list token total

`GET /api/agent-sessions` SHALL give each row the input, cache-write, cache-read and output tokens of the session's messages, counted as the session summary's `usage` counts them, and SHALL leave the total out for a session that sent no usage. When the event archive cannot be read, the list SHALL still be served, without the totals.

#### Scenario: A session's total agrees with its header

- **GIVEN** sessions on two hosts whose messages include re-sent messages, copies a fork made of another session's, and lines naming a session that does not hold them
- **WHEN** the tokens of a page of sessions are read
- **THEN** each session's total equals the sum of its summary's usage by model, and a session with no messages has none

#### Scenario: Each session is listed with its tokens

- **GIVEN** a session that sent usage and one that sent none
- **WHEN** the sessions list is read
- **THEN** the first carries its tokens and the second none
- **AND** when the event archive cannot be read, both are listed without tokens
