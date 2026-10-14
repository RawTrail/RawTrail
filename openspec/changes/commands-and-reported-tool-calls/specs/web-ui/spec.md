## ADDED Requirements

### Requirement: Observed commands and reported tool calls are named apart

The session view SHALL name its lens of observed process groups Commands, one group per command the agent ran. A tool call it shows from the agent's transcript, paired with a command or listed as one of several identical calls, SHALL be labelled as reported by the agent.

#### Scenario: A reported tool call is labelled as the agent's

- **GIVEN** a session with three commands, the first paired with a reported Bash call, the second matching two identical calls, and the third matching none
- **WHEN** the session view lists them under Commands
- **THEN** the first and second carry the "Reported by agent" label, and the third carries none
