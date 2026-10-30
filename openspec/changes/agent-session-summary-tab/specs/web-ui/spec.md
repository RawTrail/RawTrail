## ADDED Requirements

### Requirement: Agent session summary tab

The agent session view SHALL list a Summary first among its views and open on it, built only on the session summary. It SHALL describe the session's prompts and commands, tools and programs, files by directory and outside its project, destinations by organization with domains new to the host marked, tokens and permission modes, and a timeline. Alerts SHALL be a count with a link, not the headline.

#### Scenario: The summary is the first view

- **GIVEN** an agent session
- **WHEN** the operator opens it
- **THEN** it opens on its Summary, listed first among its views, which shows the session from its summary
- **AND** a session reached from another session's lineage opens on its Summary too, whichever view was open

#### Scenario: The summary describes the session

- **GIVEN** a session that was sent prompts, ran commands, wrote files in and outside its project, and talked to a Git host and a domain new to the host
- **WHEN** the operator opens its Summary
- **THEN** each section describes that, the destinations grouped by organization with the new domain marked, and the alerts are a count with a link at the end

#### Scenario: An empty session reads as empty

- **GIVEN** a session that recorded no tool calls, files, network activity or usage
- **WHEN** the operator opens its Summary
- **THEN** each section says plainly that none was recorded
