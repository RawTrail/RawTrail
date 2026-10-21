## ADDED Requirements

### Requirement: Session processes view

The session view SHALL offer a Processes lens listing every process the session ran as one tree under the agent process that ran it, with nothing hidden or folded, and the number of processes it lists. A process launchd adopted after its creator exited SHALL stay under its creator and be marked as reparented. When the agent was not seen, the commands SHALL be listed as roots.

#### Scenario: Every process is listed under the agent that ran it

- **GIVEN** a session whose agent process ran a command that forked a subshell copy of its shell, which backgrounded a sleep
- **WHEN** the operator opens the session's Processes lens
- **THEN** the agent leads, badged as the agent, with the command's shell, the subshell's copy and the sleep beneath it
- **AND** the lens counts every process it lists, the agent included

#### Scenario: A reparented process stays under its creator

- **GIVEN** a backgrounded sleep that exec'd after its subshell exited, so launchd adopted it
- **WHEN** the session's processes are listed
- **THEN** the sleep is listed under the subshell that created it and badged as reparented, and its creators are not
