## ADDED Requirements

### Requirement: Agent own processes view

The Processes view SHALL count the processes it lists below the agent, the internal ones only when shown, and offer a toggle with their count listing the processes the agent ran for itself under it, in fork order among the session's, each marked internal. They SHALL be hidden until asked for, and the toggle SHALL be absent when there are none.

#### Scenario: The agent's own processes are counted and listed on request

- **GIVEN** a session whose agent ran a plugin scan and a shell snapshot with a child
- **WHEN** the operator opens the Processes view, then turns the toggle on
- **THEN** the summary reads "1 process" and the toggle "Show internal processes (3)", none listed; then "4 processes", with all 3 under the agent marked "internal" and the command not
