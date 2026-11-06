## ADDED Requirements

### Requirement: A laptop install has personal defaults

On a server whose profile is `laptop`, the console SHALL open on Agent sessions for an operator who can read them, and Admin settings SHALL offer single sign-on, containment and service accounts only once the person turns on **Show advanced settings**, a choice the browser keeps. Every page SHALL stay reachable by its address. A `server` profile SHALL keep the alert queue first and offer every section with no switch.

#### Scenario: A laptop opens on Agent sessions

- **GIVEN** an operator who can read alerts and agent sessions
- **WHEN** they open the console's root on a laptop, and on a server
- **THEN** the laptop shows Agent sessions and the server the alert list

#### Scenario: Fleet settings are behind a switch

- **GIVEN** an operator who can open every Admin settings section, on a laptop
- **WHEN** they open Admin settings, then turn on **Show advanced settings**
- **THEN** single sign-on, containment and service accounts are offered only after the switch is on, and the browser keeps the choice

### Requirement: The first-run checklist walks the approvals from live health

The console SHALL offer a first-run checklist that lists, in order, the approvals a Mac needs to capture: the security extension, the network extension, network filtering, Full Disk Access for the security extension and for the agent, the first event, and the first agent session. Each step SHALL show whether it is done, needs the person, or is waiting, and where to make it, read from the Mac's live agent health. A refused Full Disk Access grant SHALL show as a step that needs the person.

#### Scenario: Each approval shows where it stands

- **GIVEN** a Mac whose agent reports some approvals made and others not
- **WHEN** the person opens the checklist
- **THEN** the steps are listed in order, the made ones done, and each open one says where to make it

#### Scenario: A refused Full Disk Access grant is a red step

- **GIVEN** a Mac whose security extension or agent reports `full_disk_access_missing`
- **WHEN** the person opens the checklist
- **THEN** that component's Full Disk Access step is shown red as needing the person, with where to grant it

#### Scenario: The checklist follows the Mac as it is set up

- **GIVEN** an open checklist with a step left
- **WHEN** the agent next reports the approval made
- **THEN** the step turns done without a reload, and once every step is done the checklist stops rereading and points to Agent sessions

## MODIFIED Requirements

### Requirement: Alert list is the home view

The application root SHALL route to the alert list, except on a `laptop` profile, where it SHALL route to Agent sessions for an operator who can read them. An operator who cannot read the home surface but can open another navigation entry SHALL land on the first one their permissions confer, in display order, derived from the same permission-gated navigation model that renders the entries, rather than a no-access state.

#### Scenario: Root routes to the alert list

- **GIVEN** an authenticated operator whose permission set contains `alert.read`, on a `server` profile
- **WHEN** the operator navigates to the application root
- **THEN** the UI renders the alert list

#### Scenario: Operator without alert read lands on their first permitted surface

- **GIVEN** an authenticated operator whose permission set does not contain `alert.read` but confers another navigation entry
- **WHEN** the operator navigates to the application root
- **THEN** the UI routes to the first navigation entry in display order that the operator's permission set confers
- **AND** no no-access state is rendered for the landing itself
