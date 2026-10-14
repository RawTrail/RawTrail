## ADDED Requirements

### Requirement: Agent-scoped file activity

The extension SHALL report file creates, modified closes, renames and deletes by an AI agent's processes as `file_activity` events, and SHALL NOT report them for any other process. An agent subtree SHALL start at an exec of the Claude Code binary, matched by signing identifier, or of a process carrying `CLAUDE_CODE_SESSION_ID`, and SHALL extend to every process it forks, from the fork. Each flush SHALL send at most 64 events.

#### Scenario: An agent process is observed from its exec

- **GIVEN** the extension is running
- **WHEN** the Claude Code binary execs, or a process carrying `CLAUDE_CODE_SESSION_ID` does
- **THEN** its later file operations arrive as `file_activity` events naming its `pid` and `pidversion`

#### Scenario: The observed set follows the subtree

- **GIVEN** an observed process
- **WHEN** it forks, and the child execs at once and writes a file
- **THEN** the child's write is reported, under the generation its exec created
- **AND** the child is released when it exits

#### Scenario: A process outside an agent subtree is not observed

- **GIVEN** a process that is neither an agent root nor forked from an observed process
- **WHEN** it creates, changes, renames or deletes files
- **THEN** no `file_activity` event names it

#### Scenario: Repeats of one file fold into one row

- **GIVEN** an observed process changes one file several times within a second
- **WHEN** the window is flushed
- **THEN** one event carries the file with `coalesced_count` and `last_timestamp_ns`, timestamped at the first change

#### Scenario: A busy window rolls up into directories

- **GIVEN** an observed build creates thousands of files within a second
- **WHEN** the window is flushed
- **THEN** at most 64 events are sent, the deepest paths folded into their directories with `rolled_up` set
- **AND** every operation is counted in an event or, past the top level, logged as dropped

#### Scenario: A terminal is not file activity

- **GIVEN** an observed shell command writes to its terminal
- **WHEN** it exits and closes the terminal device
- **THEN** no `file_activity` event names a path under `/dev/`
