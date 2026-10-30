## ADDED Requirements

### Requirement: Agent session bypass record

The graph builder SHALL record each host's agent session that an `agent_usage` event names in the `bypassPermissions` mode, with the time of its earliest such event, including a forked session's copy of its parent's line. Reprocessing, or events arriving in any order, SHALL leave the same record, and a session id too long to store SHALL drop only its own record.

#### Scenario: A session that ran bypassed is recorded

- **GIVEN** a session with messages in `auto` and two in `bypassPermissions`, a session in `default` and with no mode, and a fork whose copy of a bypassed line names it
- **WHEN** the events are processed in one batch, one at a time oldest first, or twice
- **THEN** the first session is recorded at its earliest bypassed message, and the fork is recorded
- **AND** the session never bypassed is not recorded, and a bypassed message with an oversized session id leaves the rest of its batch recorded
