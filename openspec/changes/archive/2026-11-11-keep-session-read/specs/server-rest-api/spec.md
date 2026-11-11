## ADDED Requirements

### Requirement: A session's process read is kept briefly

Each server replica SHALL serve an AI agent session's views (its conversation, commands, processes and summary) from one read of the session's processes for up to 30 seconds from the read's start, while the session's last activity is unchanged, and SHALL read the session again once that time passes or its last activity moves. Views of one session that the same replica serves at the same time SHALL share one read.

#### Scenario: A second view of an idle session reuses its read

- **GIVEN** a session whose last activity does not change
- **WHEN** an operator opens its Timeline, then its Commands view, then searches its processes, within 30 seconds
- **THEN** a replica that serves all three reads the session's processes once for them

#### Scenario: A session that moved on is read again

- **GIVEN** a session whose processes were read for one of its views
- **WHEN** its last activity moves, or 30 seconds pass, before the next view
- **THEN** the next view reads the session's processes again

#### Scenario: Views opened together share one read

- **GIVEN** a session whose processes are not kept
- **WHEN** several of its views ask for them at the same time
- **THEN** the server reads them once and answers every view from that read
