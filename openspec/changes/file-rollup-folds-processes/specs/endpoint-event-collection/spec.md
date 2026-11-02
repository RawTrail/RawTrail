## ADDED Requirements

### Requirement: File activity rows fold by process

When a flush's rows exceed its budget, the extension SHALL fold rows of one operation on one path into the nearest observed process their processes all descend from, keeping the path and carrying `process_count`, before it folds any into a directory, and SHALL alternate the two folds until the window fits. It SHALL NOT fold rows of two agent subtrees together, and SHALL drop rows only when neither fold reduces the window.

#### Scenario: A fan-out folds into the process that started it

- **GIVEN** an agent session's `xargs` starts 80 jobs that each close the output file they inherited within a second
- **WHEN** the window is flushed
- **THEN** one event names that file and the `xargs` process, with `process_count` 80 and every operation counted
- **AND** no operation is dropped, and the session's other rows keep their own process and path

#### Scenario: Two sessions never share a row

- **GIVEN** two agent sessions whose jobs change the same file within a second, past the budget
- **WHEN** the window is flushed
- **THEN** each session's jobs fold into a row of their own, named for a process in that session
