## ADDED Requirements

### Requirement: Agent file activity views

The activity view's Files tab SHALL list an AI agent's file operations with the process behind each, marking a rolled-up row as a directory and everything below it. The host timeline SHALL offer a Files class whose rows name the process by pid and spell out the operation, its path, a rename's source, and how many operations a row folds.

#### Scenario: The Files tab lists an agent's edits

- **GIVEN** a session whose activity holds a file modified four times and a rolled-up directory of created files
- **WHEN** the Files tab is opened
- **THEN** the file is listed as `modify` with a count of 4
- **AND** the directory is listed as `create`, marked as covering everything below it

#### Scenario: The timeline shows an agent's file activity

- **GIVEN** a host timeline holding agent file activity
- **WHEN** it is shown, or narrowed with the Files chip
- **THEN** each row spells out its operation and path, a rename with its source and a folded row with its count
- **AND** the process column names the pid rather than the file
