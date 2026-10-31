## ADDED Requirements

### Requirement: A command that read a credential says so

In the agent session view, a command whose process raised an `agent_credential_read` alert SHALL say beside it, whether open or closed, what the agent did with which credential, in the alert's words: "Tried to read your AWS credentials", or "Stopped from reading" one the guard refused, each linking to its alert.

#### Scenario: A command names the credential it read

- **GIVEN** a session command with one alert for reading AWS credentials and one for a refused read of an SSH private key
- **WHEN** an operator opens the session
- **THEN** the command says "Tried to read your AWS credentials" and "Stopped from reading your SSH private key", each linking to its alert, and still does once the command is closed
