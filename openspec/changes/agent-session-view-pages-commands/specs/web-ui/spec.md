## ADDED Requirements

### Requirement: Agent session view pages its commands

The session view's Commands SHALL list the commands the server groups for the session a page at a time, offering the next page on request with how many remain. A search there SHALL ask the server for the commands it finds once the operator stops typing. The header SHALL count the session's commands and processes from its totals. Each read's cut SHALL be told with the view it cuts.

#### Scenario: The rest of the commands come on request

- **GIVEN** a session with more commands than one page
- **WHEN** its view opens
- **THEN** it lists the first page and offers the rest, saying how many
- **AND** asking for them lists the next page after the first, and no more is offered once none remain

#### Scenario: A search asks the server

- **GIVEN** a session's commands listed
- **WHEN** the operator types a search and stops
- **THEN** the view asks the server for the commands the search finds and lists only those

#### Scenario: The header counts the whole session

- **GIVEN** a session whose totals count more commands and processes than any page lists
- **WHEN** its view opens
- **THEN** the header counts the commands and processes its totals give
