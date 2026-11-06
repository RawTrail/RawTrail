## ADDED Requirements

### Requirement: A session digest counts its unrecorded file operations

The session digest SHALL count, as `files.unrecorded`, the operations of the `file_activity_dropped` events in its scope. Where a process belongs to a session only within a span of time, as a Claude Code process that served a session after a `/clear` does, an event SHALL count toward the session when the stretch from its first to its last dropped operation overlaps that span, so a session that lost operations never reads as complete.

#### Scenario: The digest counts unrecorded file operations

- **GIVEN** two `file_activity_dropped` events for 309 and 40 operations by a session's process, and one by a process outside it
- **WHEN** the session's digest is read
- **THEN** `files.unrecorded` is 349

#### Scenario: Dropped operations across a /clear count toward both sessions

- **GIVEN** a Claude Code process that served one session and then, after a `/clear`, another, and `file_activity_dropped` events by it for 10 operations within the first session, 5 whose first and last operations fall on either side of the `/clear`, and 3 within the second
- **WHEN** each session's digest is read
- **THEN** `files.unrecorded` is 15 for the first session and 8 for the second
