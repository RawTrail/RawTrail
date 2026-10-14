## ADDED Requirements

### Requirement: Session tree names its agent

A session's tree read SHALL return the session's agent processes in `agents`: the creator of a root whose pid is the session's agent pid. Its `roots`, counts and truncation SHALL be those of the session's own processes. Other reads SHALL return no `agents`.

#### Scenario: A session's tree names the agent that ran its commands

- **GIVEN** a session whose agent created two of its roots, and a root another process created
- **WHEN** the session's tree is read
- **THEN** `agents` holds the agent's process, and the three roots stay roots
- **AND** a story read returns no `agents`, and a failed session lookup fails the read
