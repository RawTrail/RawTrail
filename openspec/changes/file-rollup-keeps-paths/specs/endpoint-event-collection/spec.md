## ADDED Requirements

### Requirement: File activity rows keep their paths

When a flush folds `file_activity` rows into directories to fit its budget, a row that merged with no other SHALL leave the flush as it entered it, keeping its path and its `rolled_up` flag, and a row that merged several SHALL name the deepest directory their paths share.

#### Scenario: A row that folds nothing keeps its path

- **GIVEN** more processes than the budget each close the same inherited output file within a second
- **WHEN** the window is flushed
- **THEN** every event sent names that file, none of them `rolled_up`
- **AND** the operations past the budget are logged as dropped

#### Scenario: A folded row names the directory its paths share

- **GIVEN** a busy window in which one process writes several files in one directory
- **WHEN** the window is flushed and its rows are folded
- **THEN** that process's files arrive as one `rolled_up` event naming that directory, not a directory above it
