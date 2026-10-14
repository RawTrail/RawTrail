## ADDED Requirements

### Requirement: Agent tool calls endpoint

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls` SHALL list the tool calls the session recorded, oldest first, each `tool_use_id` once at the time it was first recorded, read over the session's span widened by an hour on each side, and SHALL say when the list was cut. An unknown session SHALL be answered with `404 not_found`.

#### Scenario: A session's tool calls are listed once each

- **GIVEN** a session whose agent reported one call twice, around a restart, beside another session's call and another host's
- **WHEN** its tool calls are read
- **THEN** each of its calls is listed once, at its first recording, oldest first
- **AND** no other session's or host's call is listed
