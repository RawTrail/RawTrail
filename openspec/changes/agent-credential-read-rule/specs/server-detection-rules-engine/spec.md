## ADDED Requirements

### Requirement: An AI agent reading credentials is reported

The detection engine SHALL raise a high-severity `agent_credential_read` alert for each `agent_credential_would_block` report, naming the opener (the process generation the report names) and the credential in plain words, one alert per AI agent session and kind of credential; a process with no session SHALL be alerted on its own. An unnamed kind SHALL read as a credential file. The opener SHALL be excludable by path, team, signing identifier and cdhash.

#### Scenario: A session reading a credential is alerted once per kind

- **GIVEN** a Claude Code session whose cat read two SSH private keys and whose gh read the GitHub CLI token, and the agent's own process, with no session, reading a key
- **WHEN** the reports are evaluated
- **THEN** the two key reads share one alert's subject, the token read has its own, and the agent's process keeps its own
- **AND** the alert says "An AI agent's /bin/cat tried to read your SSH private key", and a kind from a newer agent reads "a credential file"

#### Scenario: A report is the generation it names

- **GIVEN** a report of pid 6991's generation running cat, and pid 6991's earlier generation, the shell that execed cat, in another session
- **WHEN** the report is evaluated
- **THEN** the alert is cat's, in cat's session
- **AND** a report naming a generation not yet recorded is retried rather than resolved to another, and a session id at the marker's 256-byte limit still yields a subject the alert column holds
