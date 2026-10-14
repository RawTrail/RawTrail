## ADDED Requirements

### Requirement: AI agent sessions page

The UI SHALL offer a Sessions page listing AI agent sessions most recently active first, showing per session the agent and its version, the project directory, the host, first and last seen, and its process and alert counts. Its host, agent and activity-window filters SHALL live in the URL and be applied by the server, which SHALL also page the list. A session SHALL open its session view.

#### Scenario: Sessions are listed with their project and activity

- **GIVEN** the server reports a Claude Code session on a host, working in `/private/tmp`, with 16 processes and 1 alert
- **WHEN** the operator opens the Sessions page
- **THEN** the row shows the agent and version, `/private/tmp`, the host's name, the first and last seen times, 16 and 1

#### Scenario: Filters and paging are applied by the server

- **GIVEN** the Sessions page with host, agent and "active in the last 7 days" filters in the URL
- **WHEN** the page loads, and then the operator asks for more
- **THEN** the server receives the host, the agent and a window start 7 days before now
- **AND** the next page is requested with the server's cursor and appended to the rows already shown

#### Scenario: A session opens its activity

- **GIVEN** a listed session
- **WHEN** the operator activates it
- **THEN** that session's view opens

## MODIFIED Requirements

### Requirement: Alert-first navigation order

The top navigation SHALL present its entries in the order Alerts, Hosts, Sessions, Search, Application control, Rules, subject to the capability gating that hides entries the operator cannot read. The Hosts entry SHALL be active on the host list and a host's page. An entry whose section holds more than one surface SHALL be active on any of them, so an operator moving between them is not told they have left it.

#### Scenario: Navigation lists Alerts first

- **GIVEN** an authenticated operator whose permission set confers every navigation entry
- **WHEN** the authenticated application renders its navigation
- **THEN** the entries appear in the order Alerts, Hosts, Sessions, Search, Application control, Rules

#### Scenario: Hosts entry active on host detail

- **GIVEN** an authenticated operator viewing a host's process tree page
- **WHEN** the navigation renders
- **THEN** the Hosts entry is highlighted as the active entry

#### Scenario: The Rules entry stays active on coverage

- **GIVEN** an authenticated operator viewing the coverage surface
- **WHEN** the navigation renders
- **THEN** the Rules entry is highlighted as the active entry
