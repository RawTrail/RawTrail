## ADDED Requirements

### Requirement: Dropped file activity is reported

When a flush drops `file_activity` rows past its budget, the extension SHALL send a `file_activity_dropped` event for each agent subtree that lost operations, naming the topmost observed process of its session, with the number of operations dropped and the time of the first and last of them. It SHALL name at most as many subtrees a flush as rows it sends, and SHALL count the operations of any further subtrees in one event that names no process.

#### Scenario: A session's dropped operations are reported

- **GIVEN** two agent sessions' rows in one second, more than the budget after every fold
- **WHEN** the window is flushed and one session's row is dropped
- **THEN** a `file_activity_dropped` event names that session's root with the operations its row stood for
- **AND** no such event names the session whose rows were all sent

#### Scenario: Loss reports are bounded like rows

- **GIVEN** more agent subtrees lose operations in one second than the budget
- **WHEN** the window is flushed
- **THEN** events name at most the budget's number of subtrees
- **AND** one more event, naming no process, counts the operations of the rest
