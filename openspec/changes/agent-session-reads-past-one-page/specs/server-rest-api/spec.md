## ADDED Requirements

### Requirement: Agent session reads past one page

The server SHALL compute an AI agent session's commands, its summary and its activity rollup over up to 20,000 of its processes, read a chunk at a time in fork order, and SHALL report a cut read for a session with more. A client's session tree read SHALL stay bounded at its page. A scoped read's total SHALL be counted to 100,000.

#### Scenario: A session past the tree page is read whole

- **GIVEN** a session of more than 12,000 processes, several sharing each fork time
- **WHEN** the server reads it for its commands, its summary or its activity
- **THEN** every process is read once, in fork order with ties by row, across every chunk
- **AND** the activity is matched over every one of their generations
- **AND** a read cut shorter than the session still reports the session's total

### Requirement: Agent session tree pages

The process-forest endpoint's `scope=session` and `scope=story` reads SHALL page by an opaque `cursor`: a cut page SHALL name the cursor of the next, read from after its last row in fork order, each page SHALL report the scope's total, and a session's agent processes SHALL lead its first page only. A malformed cursor SHALL be rejected with `invalid_cursor`.

#### Scenario: A session tree pages by cursor

- **GIVEN** a session with more processes than one page
- **WHEN** its tree is read page by page, each from the last page's cursor
- **THEN** every process is read once across the pages, each page reports the session's total, and the last names no cursor
- **AND** the agent processes are read with the first page only, and a malformed cursor is rejected

