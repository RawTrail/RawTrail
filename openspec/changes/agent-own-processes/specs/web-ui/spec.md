## ADDED Requirements

### Requirement: Agent own processes view

The Processes view SHALL count the processes from the session's commands and, when the agent ran internal processes, lead with the total; it SHALL name the agent by its product name and offer a toggle listing its internal processes under it, in fork order among the session's, each marked internal. They SHALL be hidden until asked for, and the toggle SHALL be absent when there are none.

#### Scenario: The agent's own processes are counted and listed on request

- **GIVEN** a session whose agent ran a plugin scan and a shell snapshot with a child
- **WHEN** the operator opens the Processes view, then turns the toggle on
- **THEN** the summary reads "5 processes in all: 1 from the session's commands, 3 internal to Claude Code, and Claude Code itself", the toggle offers the 3 and none is listed, then all 3 are listed under the agent marked "internal", and the command is not
