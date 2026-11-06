## REMOVED Requirements

### Requirement: AI agent sessions page

**Reason**: Its filters are removed from the page, and a MODIFIED block cannot drop the scenario that stated them.

**Migration**: Replaced by "Agent sessions list", which keeps the other two scenarios, shows how long ago a session was last active and how long it ran in place of its first and last seen times, and keeps the server's paging as its own scenario.

## ADDED Requirements

### Requirement: Agent sessions list

The UI SHALL offer an Agent sessions page listing AI agent sessions most recently active first, showing per session the agent and its version, the project directory, the host, how long ago it was last active, how long it ran, and its process and alert counts. Each time SHALL stay on one line, with the exact times on hover. The server SHALL page the list. A session SHALL open its session view.

#### Scenario: Sessions are listed with their project and activity

- **GIVEN** the server reports a Claude Code session on a host, working in `/private/tmp`, with 16 processes and 1 alert, last active 3 days ago after running for 2 hours 14 minutes
- **WHEN** the operator opens the Agent sessions page
- **THEN** the row shows the agent and version, `/private/tmp`, the host's name, `3d ago`, `2h 14m`, 16 and 1
- **AND** hovering the times shows when the session was last active, and when it started and was last active

#### Scenario: The server pages the list

- **GIVEN** more sessions than one page holds
- **WHEN** the operator asks for more
- **THEN** the next page is requested with the server's cursor and appended to the rows already shown

#### Scenario: A session opens its activity

- **GIVEN** a listed session
- **WHEN** the operator activates it
- **THEN** that session's view opens
