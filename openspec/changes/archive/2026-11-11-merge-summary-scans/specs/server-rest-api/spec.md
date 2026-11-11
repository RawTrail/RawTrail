## ADDED Requirements

### Requirement: A session summary's reads run together

The server SHALL make the reads of an AI agent session's summary that do not depend on the session's processes (its process and alert counts, tool calls, usage and prompt count) while it reads the session's processes and the digest of its activity, and SHALL count the session's activity totals in the same pass over its events as the digest's timeline.

#### Scenario: The reads that do not need the session's processes run beside them

- **GIVEN** a session whose every read takes a while to answer
- **WHEN** an operator opens its Summary
- **THEN** the server starts its counts, tool calls, usage, prompt count and digest reads before any of them has answered, and answers the same totals as when they run one after another
