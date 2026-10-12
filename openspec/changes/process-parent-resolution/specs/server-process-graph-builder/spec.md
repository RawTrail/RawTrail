## ADDED Requirements

### Requirement: Process creator resolution

The graph builder SHALL set `parent_process_id` from a fork's `(parent_pid, parent_pidversion)`: the row of that exact generation that had forked by the child's fork, else the parent's row in force then, unless that row is a newer generation whose exec preceded the fork. A process first seen at exec SHALL link the same way to the parent it names. A fork without `parent_pidversion`, or a reparented process first seen at exec, SHALL leave it unset.

#### Scenario: The exact generation wins over a reused pid

- **GIVEN** a parent pid was reused by a later process that forked before the child's fork timestamp
- **WHEN** the child's fork names the earlier process's generation
- **THEN** the child links to the earlier process's row

#### Scenario: A child forked before its parent's first exec links to that row

- **GIVEN** a parent's first exec rewrote its row's generation after the child forked
- **WHEN** the child's fork names the generation the exec replaced
- **THEN** the child links to the parent's row

#### Scenario: A newer generation is not a creator

- **GIVEN** the only row for the parent pid is a later process that had exec'd before the child forked
- **WHEN** the child's fork names an older generation
- **THEN** the child's link is unset

#### Scenario: A daemon links to its creator after a double fork

- **GIVEN** a shell forked a subshell, which forked a daemon and exited before the daemon exec'd with `ppid` 1
- **WHEN** the events are processed, in one batch or one at a time
- **THEN** the daemon links to the subshell's row and the subshell to the shell's
- **AND** a re-exec of the daemon keeps its link

#### Scenario: Events without a generation leave the link unset

- **GIVEN** a fork without `parent_pidversion`, or a reparented process first seen at exec
- **WHEN** it is processed
- **THEN** its link is unset
- **AND** a process first seen at exec that names its parent's generation links to that parent

### Requirement: Exit binding by process generation

An exit that carries `pidversion` SHALL close the open row of that generation for its pid, else the most recent open row whose generation is older or unknown, and SHALL NOT close a row of a newer generation. A buffered exit SHALL NOT be taken by a snapshot exec of a newer generation. An exit without `pidversion` SHALL keep the pid-only rule.

#### Scenario: An exit never closes a newer generation

- **GIVEN** a later process on a reused pid is the open row with a newer generation
- **WHEN** a late exit for the earlier generation arrives
- **THEN** the later process's row stays open
- **AND** when two rows are open, the exit closes the one of its own generation

#### Scenario: A buffered exit skips a newer snapshot

- **GIVEN** an exit for a generation with no row was buffered
- **WHEN** a snapshot exec of a newer generation arrives for the same pid
- **THEN** the snapshot row stays open
