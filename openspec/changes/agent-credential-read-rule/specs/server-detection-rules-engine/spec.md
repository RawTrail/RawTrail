## ADDED Requirements

### Requirement: An AI agent reading credentials is reported

The detection engine SHALL raise a high-severity `agent_credential_read` alert for each `agent_credential_would_block` report, naming the opener and the credential in plain words, keyed so that one AI agent session's reports of one kind of credential are one alert; a process with no session SHALL be alerted on its own. A kind the engine does not name SHALL be alerted as a credential file. The opener SHALL be excludable by path, team, signing identifier and cdhash.

#### Scenario: A session reading a credential is alerted once per kind

- **GIVEN** a Claude Code session whose cat read two SSH private keys and whose gh read the GitHub CLI token, and the agent's own process, with no session, reading a key
- **WHEN** the reports are evaluated
- **THEN** the two key reads share one alert's subject, the token read has its own, and the agent's process keeps its own
- **AND** the alert says "An AI agent's /bin/cat tried to read your SSH private key", and a kind from a newer agent reads "a credential file"
