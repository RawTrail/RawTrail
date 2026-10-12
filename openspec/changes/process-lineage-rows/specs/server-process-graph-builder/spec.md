## ADDED Requirements

### Requirement: Process lineage persistence

The graph builder SHALL store on a forked row the forking process's `parent_pidversion`, and on a row an exec images the exec's `original_ppid`, `responsible_pid`, `responsible_pidversion`, `session_id`, `group_id`, `tty`, `start_time_ns` and non-empty `cwd`. It SHALL mark the row `reparented` when the exec reports `ppid` 1 and an `original_ppid` above 1. An exec SHALL NOT change a row's `ppid` or `parent_pidversion`.

#### Scenario: An exec stores its lineage on the row

- **GIVEN** a fork carrying `parent_pidversion` created a row
- **WHEN** the first exec for that process arrives with the lineage fields
- **THEN** the row stores each lineage field the exec reported
- **AND** a later re-exec on the same pid keeps the row's `ppid` and `parent_pidversion` and stores the new exec's lineage

#### Scenario: A daemonized process is marked reparented

- **GIVEN** a process was forked by an intermediate parent that exited before the process exec'd
- **WHEN** its exec reports `ppid` 1 and the intermediate parent's pid in `original_ppid`
- **THEN** the row is marked `reparented`
- **AND** it keeps the `ppid` and `parent_pidversion` its fork recorded

#### Scenario: A legacy event leaves the lineage unset

- **GIVEN** an agent that sends no lineage fields
- **WHEN** its fork and exec events are processed
- **THEN** every lineage column on the row is unset and `reparented` is false
- **AND** an empty `cwd` is stored as unset
