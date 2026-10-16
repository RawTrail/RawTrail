## ADDED Requirements

### Requirement: Agent tool call origin

When a transcript line records a tool call under a `session_id` other than its session's, the agent SHALL send that id as the call's `origin_session_id`. A line whose `session_id` is its session's, or absent, SHALL carry none.

#### Scenario: A copied call names the session it was written under

- **GIVEN** a forked session's transcript, whose copy of its parent's tool call keeps the parent's `session_id`
- **WHEN** the agent reads the line
- **THEN** it reports the call under the fork's session with `origin_session_id` naming the parent, and reports the fork's own calls with none
