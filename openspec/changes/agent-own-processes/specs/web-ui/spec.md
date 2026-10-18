## ADDED Requirements

### Requirement: Agent own processes view

The Processes view SHALL count the processes from the session's commands, name the agent by its product name, count the processes it ran for itself, and offer a toggle listing those under the agent, in fork order among the session's processes, each marked internal. They SHALL be hidden until asked for, and the toggle SHALL be absent when there are none.

#### Scenario: The agent's own processes are counted and listed on request

- **GIVEN** a session whose agent ran a plugin scan and a shell snapshot with a child
- **WHEN** the operator opens the Processes view, then turns the toggle on
- **THEN** the summary counts the command's 1 process, the toggle offers Claude Code's 3 internal processes and none is listed, then all 3 are listed under the agent marked "internal", and the session's command is not
