## ADDED Requirements

### Requirement: Process forest links by creator

The process forest SHALL link a process to its creator row, and SHALL return it as a root when that row is not in the result. A process without one SHALL link to the row in the result with its exact `(ppid, parent_pidversion)`, else to the newest row holding its `ppid`. A pinned process's ancestor walk SHALL follow creator rows, ending where one is not stored. A chain's descendants SHALL exclude a child whose creator row is another process.

#### Scenario: The creator row beats a reused pid

- **GIVEN** a result holding a process's creator and a later process that reused the creator's pid
- **WHEN** the forest is built
- **THEN** the process hangs under its creator

#### Scenario: A creator outside the result leaves the process a root

- **GIVEN** a process whose creator row is not in the result, and a later process holding the creator's pid that is
- **WHEN** the forest is built
- **THEN** the process is a root

#### Scenario: An exact generation beats a reused pid

- **GIVEN** a process with no creator row whose `(ppid, parent_pidversion)` names a row in the result, and a later process holding that pid
- **WHEN** the forest is built
- **THEN** the process hangs under the row of that generation

#### Scenario: The ancestor walk follows creators through a double fork

- **GIVEN** a daemon whose creator exited before launchd adopted it, and a later process that reused the creator's pid
- **WHEN** the forest or the chain is read pinning the daemon
- **THEN** its ancestors are its creator and the creator's own creator
- **AND** a creator that retention removed ends the chain

#### Scenario: A chain excludes a child of another process

- **GIVEN** a process and a child whose `ppid` matches it inside its lifetime but whose creator row is another process
- **WHEN** the process's chain is read
- **THEN** the child is not among its descendants
