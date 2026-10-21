## ADDED Requirements

### Requirement: Process lineage identity capture

The `fork` event SHALL carry the forking process's `parent_pidversion`. The `exec` event SHALL carry `original_ppid`, the `parent_pidversion` of the parent its `ppid` names, `responsible_pid`, `responsible_pidversion`, `session_id`, `group_id`, `tty` when the process has a controlling terminal, `start_time_ns`, and the working directory in `cwd`. Each lineage field is optional on the wire and SHALL be omitted when unknown.

#### Scenario: A fork names the generation of its creator

- **GIVEN** the endpoint event capture is running
- **WHEN** a process forks a child
- **THEN** the `fork` event carries `parent_pid` and `parent_pidversion` equal to the forking process's own pid and generation
- **AND** they identify the creator even after the child is reparented to launchd

#### Scenario: A daemonized process still names its creator

- **GIVEN** the endpoint event capture is running
- **WHEN** a shell starts a command with a double fork and the command execs after its intermediate parent has exited
- **THEN** the command's `exec` event carries `ppid` 1, because launchd adopted it
- **AND** it carries the intermediate parent's pid in `original_ppid`

#### Scenario: An exec carries its grouping identity

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs
- **THEN** the `exec` event carries `session_id`, `group_id`, `start_time_ns`, `responsible_pid`, and `responsible_pidversion`
- **AND** it carries `tty` only when the process has a controlling terminal
- **AND** `cwd` holds the process's working directory at exec

#### Scenario: A snapshot row carries what the process table knows

- **GIVEN** the extension has started and enumerates the processes already running
- **WHEN** it emits a synthetic `exec` event for one of them
- **THEN** the event carries the process's `group_id` and `start_time_ns`
- **AND** it omits the lineage fields the process table cannot supply

#### Scenario: An event without lineage keeps its shape

- **GIVEN** a payload built without any lineage field
- **WHEN** it is serialized
- **THEN** none of the lineage keys appear in it
- **AND** a payload that carries them still decodes on a server that does not read them
