## ADDED Requirements

### Requirement: Agent sessions list

`GET /api/agent-sessions` SHALL list AI agent sessions most recently active first, each with its agent, version, session id, host, project directory, first and last seen, and retained process and alert counts, filtered by host, agent and the window it was active in, paged by an opaque keyset cursor with the filtered total. It SHALL require fleet-wide process read and reject a malformed cursor or an inverted window.

#### Scenario: Sessions are listed with what they did

- **GIVEN** agent sessions on two hosts, one with processes and alerts raised on them
- **WHEN** the sessions list is read
- **THEN** each session appears once, most recently active first, with its project directory, times, process count and alert count
- **AND** monitor records are not counted as alerts

#### Scenario: Filters narrow the list

- **GIVEN** sessions of different agents on different hosts at different times
- **WHEN** the list is read with a host, an agent, or a window
- **THEN** only matching sessions are returned, with their total

#### Scenario: The list pages by cursor

- **GIVEN** more sessions than the page limit
- **WHEN** the list is read page after page with each page's cursor
- **THEN** every session is returned exactly once when none changes between pages
- **AND** no session is returned twice when one becomes active between pages, since it moves ahead of the cursor
- **AND** a malformed cursor, or one missing any of its members, is rejected
