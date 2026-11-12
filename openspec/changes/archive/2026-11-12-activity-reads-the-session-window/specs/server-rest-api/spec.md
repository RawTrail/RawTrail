## MODIFIED Requirements

### Requirement: Activity rollup bounded to its scope's start

The activity rollup SHALL read only events at or after the scope's start: the earliest start or fork of its processes, except that an AI agent process bounded to the span it served a session counts from that span's start. An older process that shared a generation, as one from before a reboot can, is then not counted. A snapshot process with no recorded start, or a process with no time, SHALL leave the read unbounded.

#### Scenario: An earlier process sharing a generation is not counted

- **GIVEN** a scope's process, and an earlier process with the same pid and pidversion that opened, connected and resolved before the scope's process started
- **WHEN** the scope's activity is read
- **THEN** the totals and lists hold only the scope's process's events, read from the earliest fork among the processes read

#### Scenario: A scope whose start is not known is read unbounded

- **GIVEN** a scope holding a snapshot process with no recorded start, or a process with no time
- **WHEN** its activity is read
- **THEN** the read is not bounded by time

#### Scenario: An agent bounded to a later session bounds it from that session

- **GIVEN** a session served by an agent process that started days earlier for another session, and whose other processes started after the agent's first touch of this session
- **WHEN** the session's activity is read
- **THEN** the read starts at the agent's first touch of this session, not at the agent's own start
- **AND** an event the agent recorded over a stretch that began before that touch and ended after it is still counted

## ADDED Requirements

### Requirement: An agent's session touches are kept briefly

Each server replica SHALL keep the session touches of an AI agent process generation, which bound the span it served each session, for up to 30 seconds from their read, and reads of the same generation's touches that the same replica serves at the same time SHALL share one read. A failed read SHALL NOT be kept.

#### Scenario: A session's reads look up its agent's touches once

- **GIVEN** a session served by an agent process that served another session before it
- **WHEN** an operator opens its Activity tab, which reads the totals and each class's first page
- **THEN** a replica that serves those reads looks up the agent's session touches once for them
- **AND** a lookup that failed is made again by the next read
