## ADDED Requirements

### Requirement: Dropped file activity is reported

When a flush drops `file_activity` rows past its budget, the extension SHALL send a `file_activity_dropped` event for each agent subtree that lost operations, naming the topmost observed process of its session, with the number of operations dropped and the time of the first and last of them.

#### Scenario: A session's dropped operations are reported

- **GIVEN** two agent sessions' rows in one second, more than the budget after every fold
- **WHEN** the window is flushed and one session's row is dropped
- **THEN** a `file_activity_dropped` event names that session's root with the operations its row stood for
- **AND** no such event names the session whose rows were all sent
