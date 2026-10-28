## ADDED Requirements

### Requirement: Agent session reads past one page

The server SHALL compute an AI agent session's commands, its summary and its activity rollup over up to 20,000 of its processes, read a chunk at a time in fork order, and SHALL report a cut read for a session with more. A client's session tree read SHALL stay bounded at its page. A scoped read's total SHALL be counted to 100,000.

#### Scenario: A session past the tree page is read whole

- **GIVEN** a session of more than 12,000 processes, several sharing each fork time
- **WHEN** the server reads it for its commands, its summary or its activity
- **THEN** every process is read once, in fork order with ties by row, across every chunk
- **AND** the activity is matched over every one of their generations
- **AND** a read cut shorter than the session still reports the session's total
