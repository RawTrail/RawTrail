## ADDED Requirements

### Requirement: Forked session lineage

The agent session view SHALL link a forked session to the session it was forked from, and a session to the sessions forked from it, under the session header. It SHALL show neither when the session has no lineage or the lineage read fails.

#### Scenario: A forked session links to its parent and its forks

- **GIVEN** a session forked from another and itself forked twice
- **WHEN** the operator opens it
- **THEN** the view shows "Forked from" linking the parent's view and "Forked into" linking each fork's view
