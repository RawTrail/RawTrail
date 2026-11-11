## MODIFIED Requirements

### Requirement: Agent sessions list

The UI SHALL offer an Agent sessions page listing AI agent sessions most recently active first, showing per session the agent and its version, the project directory, the host, how long ago it was last active, how long it ran, and its process and alert counts. A process count the server capped SHALL read as the count followed by `+`. Each time SHALL stay on one line, with the exact times on hover. The server SHALL page the list. A session SHALL open its session view.

#### Scenario: Sessions are listed with their project and activity

- **GIVEN** the server reports a Claude Code session on a host, working in `/private/tmp`, with 16 processes and 1 alert, last active 3 days ago after running for 2 hours 14 minutes
- **WHEN** the operator opens the Agent sessions page
- **THEN** the row shows the agent and version, `/private/tmp`, the host's name, `3d ago`, `2h 14m`, 16 and 1
- **AND** hovering the times shows when the session was last active, and when it started and was last active

#### Scenario: A capped process count says there are more

- **GIVEN** the server reports one session's process count as 10,000 and capped, and another's as 9,999
- **WHEN** the operator opens the Agent sessions page
- **THEN** the first reads `10,000+`, and hovering it says there are more than 10,000 processes
- **AND** the second reads `9,999` with nothing on hover

#### Scenario: The server pages the list

- **GIVEN** more sessions than one page holds
- **WHEN** the operator asks for more
- **THEN** the next page is requested with the server's cursor and appended to the rows already shown

#### Scenario: A session opens its activity

- **GIVEN** a listed session
- **WHEN** the operator activates it
- **THEN** that session's view opens
