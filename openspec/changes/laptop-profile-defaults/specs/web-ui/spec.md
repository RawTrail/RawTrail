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
