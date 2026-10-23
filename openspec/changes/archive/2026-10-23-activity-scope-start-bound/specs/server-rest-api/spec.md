## ADDED Requirements

### Requirement: Activity rollup bounded to its scope's start

The activity rollup SHALL read only events at or after the scope's start, the earliest start or fork of its processes, so an older process that shared a generation, as one from before a reboot can, is not counted. A snapshot process with no recorded start, or a process with no time, SHALL leave the read unbounded.

#### Scenario: An earlier process sharing a generation is not counted

- **GIVEN** a scope's process, and an earlier process with the same pid and pidversion that opened, connected and resolved before the scope's process started
- **WHEN** the scope's activity is read
- **THEN** the totals and lists hold only the scope's process's events, read from the earliest fork among the processes read

#### Scenario: A scope whose start is not known is read unbounded

- **GIVEN** a scope holding a snapshot process with no recorded start, or a process with no time
- **WHEN** its activity is read
- **THEN** the read is not bounded by time
