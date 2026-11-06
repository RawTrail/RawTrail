## ADDED Requirements

### Requirement: Agent session summary tab

The agent session view SHALL list a Summary first among its views and open on it, built only on the session summary. It SHALL show the session as the card it is shared as: the tool calls its log reports beside the processes that ran, the files it changed, the domains it contacted, the programs it ran most and where it talked to. Alerts SHALL be a count with a link, and the Summary SHALL say how many of Claude Code's own files it left out.

#### Scenario: The summary is the first view

- **GIVEN** an agent session
- **WHEN** the operator opens it
- **THEN** it opens on its Summary, listed first among its views, which shows the session from its summary
- **AND** a session reached from another session's lineage opens on its Summary too, whichever view was open

#### Scenario: The summary describes the session

- **GIVEN** a session that made tool calls, ran processes, changed files in and outside its project, made temporary files, wrote Claude Code's own files, and talked to a Git host
- **WHEN** the operator opens its Summary
- **THEN** its card counts the tool calls beside the processes, the files changed with those outside the project and the temporary ones left out, and the domains, and groups the Git host by organization
- **AND** the alerts are a count with a link, and the Summary says how many of Claude Code's own files were left out

#### Scenario: An empty session reads as empty

- **GIVEN** a session that recorded no tool calls, processes, files, network activity or usage
- **WHEN** the operator opens its Summary
- **THEN** its card counts zero of each and lists no programs or destinations, and the Summary says no alert was raised
