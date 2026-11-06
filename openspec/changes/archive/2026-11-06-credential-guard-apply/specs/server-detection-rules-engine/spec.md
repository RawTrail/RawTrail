## ADDED Requirements

### Requirement: A refused credential read is alerted as stopped

The `agent_credential_read` rule SHALL alert an `agent_credential_block` event as it does an `agent_credential_would_block` one, under the same subject, saying the agent was stopped from reading the credential.

#### Scenario: A refused read is alerted as stopped

- **GIVEN** an agent session's `cat` refused an SSH key in protect mode, and another of its processes allowed to read one in detect mode
- **WHEN** the rule evaluates both reports
- **THEN** the first alert says the agent was stopped from reading the key and the second that it tried to read one
- **AND** both carry the session's subject for SSH keys
