## ADDED Requirements

### Requirement: Agent file activity reads

The activity rollup SHALL list `file_activity` events with the watched-path file events, taking the operation from the payload, weighing each event by its `coalesced_count`, ending a row at its latest `last_timestamp_ns`, and marking a directory row `rolled_up`. A session's activity SHALL include the creator of a root whose pid is the session's agent pid. The host timeline SHALL include `file_activity` among its event classes.

#### Scenario: Agent file activity is totalled by the operations it stands for

- **GIVEN** a process generation with a `modify` of one file folded three times, the same `modify` once more, a single `rename`, and a rolled-up `create` of a directory standing for 1856 files
- **WHEN** its activity is read
- **THEN** the files total counts 1861 operations on 3 paths
- **AND** the file's row counts 4 operations from its first to its last
- **AND** the directory's row is marked `rolled_up`

#### Scenario: The timeline lists agent file activity

- **GIVEN** a host with an exec, a `file_activity` event and a watched-path `open` event
- **WHEN** its timeline is read with no type filter, then with `type=file_activity`
- **THEN** the first read returns the exec and the file activity but not the `open`
- **AND** the second returns the file activity alone

#### Scenario: A session's activity includes the agent's own edits

- **GIVEN** a Claude Code session whose tool calls' shells were created by the `claude` process, whose pid the session's `CLAUDE_PID` marker names
- **WHEN** the session's activity is read
- **THEN** the `claude` process's generation is among those read, once however many calls it started
- **AND** a root created by any other process does not bring its creator in
