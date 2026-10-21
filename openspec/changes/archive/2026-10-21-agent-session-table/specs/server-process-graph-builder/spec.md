## ADDED Requirements

### Requirement: Agent session table

The graph builder SHALL keep one `agent_sessions` row per host and agent session, upserted from every exec carrying a session marker. Its first and last seen times SHALL span every such exec, and its agent pid, cwd, name and version SHALL come from the earliest, ties broken by event id. Reprocessing, or events arriving in any order, SHALL leave the same row. A `CLAUDE_PID` that is not a 32-bit integer SHALL leave the pid unset.

#### Scenario: A session row spans its tool calls

- **GIVEN** two tool calls of one session at different times and in different directories, and one of another session
- **WHEN** the events are processed, in one batch, one at a time, out of order, or twice, including two tool calls stamped at the same nanosecond
- **THEN** each session has one row whose first and last seen span its tool calls and whose cwd is the earliest tool call's
- **AND** a helper without a session marker and an oversized marker add no session, and an oversized `CLAUDE_PID` leaves the session's pid unset
