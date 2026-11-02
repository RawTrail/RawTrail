## ADDED Requirements

### Requirement: A session digest counts its unrecorded file operations

The session digest SHALL count, as `files.unrecorded`, the operations of the `file_activity_dropped` events in its scope.

#### Scenario: The digest counts unrecorded file operations

- **GIVEN** two `file_activity_dropped` events for 309 and 40 operations by a session's process, and one by a process outside it
- **WHEN** the session's digest is read
- **THEN** `files.unrecorded` is 349
