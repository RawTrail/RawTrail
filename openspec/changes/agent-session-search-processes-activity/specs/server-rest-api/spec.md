## ADDED Requirements

### Requirement: Agent session process search

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/processes` SHALL list the session's processes whose path or command line holds `q`, regardless of case, over the whole session read, each without its children, in fork order. It SHALL page them by `cursor`, report the matches' total and whether the session read was cut, and refuse a malformed cursor with 400 `invalid_cursor`.

#### Scenario: A search finds a process on any page of the session

- **GIVEN** a session whose matching processes lie under different commands and depths
- **WHEN** an operator searches it
- **THEN** each matching process is listed once, alone, in fork order, with the matches' total

#### Scenario: A search pages by cursor

- **GIVEN** a search that matches more processes than a page holds
- **WHEN** the client follows `next_cursor` to the end
- **THEN** it reads every match once, and a malformed cursor is refused with 400 `invalid_cursor`
