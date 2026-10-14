## ADDED Requirements

### Requirement: AI agent session view navigation

The session view SHALL fold identical processes under one creator that started nothing, changed no privilege and carry no alert into one row with their count and time span, and SHALL NOT fold when its read left processes or alerts out. It SHALL list at most 20 rows per list until asked for more, except rows holding a search hit or an alert. A search SHALL expand the calls it finds something in and count the matches per call. Selecting a process SHALL open its detail beside the session.

#### Scenario: Identical processes are folded

- **GIVEN** a tool call that ran `wc -l f` three times
- **WHEN** the operator opens the session's view
- **THEN** one row lists `wc -l f` with "×3" and the span from its first to its last fork

#### Scenario: A long list shows more on request

- **GIVEN** a tool call that ran 25 different commands
- **WHEN** the operator opens the session's view
- **THEN** the call lists 20 of them and a control to show the other 5
- **AND** a search that matches one of the other 5 lists it without the control

#### Scenario: Search expands to what it finds

- **GIVEN** a session of 11 tool calls, all collapsed, one of which ran `echo 7`
- **WHEN** the operator searches for `echo 7`
- **THEN** that call expands, says "1 match", and highlights the matching row, while the others stay collapsed

#### Scenario: A process opens its detail beside the session

- **GIVEN** the session's view
- **WHEN** the operator selects a process
- **THEN** its detail opens beside the session, with a link to it in its host's graph
- **AND** its "Show in timeline" link opens the host's timeline on a window that includes its last observed activity
