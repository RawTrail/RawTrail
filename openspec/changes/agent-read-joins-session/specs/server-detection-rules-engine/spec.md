## MODIFIED Requirements

### Requirement: An AI agent reading credentials is reported

The detection engine SHALL raise a high-severity `agent_credential_read` alert for each `agent_credential_would_block` report, naming the opener and the credential in plain words, one alert per AI agent session and kind of credential. The agent's own process SHALL count in the session it leads. A process in no session SHALL be alerted on its own. An unnamed kind SHALL read as a credential file. The opener SHALL be excludable by path, team, signing identifier and cdhash.

#### Scenario: A session reading a credential is alerted once per kind

- **GIVEN** a Claude Code session whose cat read two SSH private keys and whose gh read the GitHub CLI token, the agent's own process, which leads the session, reading a key, and a process in no session reading a key
- **WHEN** the reports are evaluated
- **THEN** the three key reads in the session share one alert's subject, the token read has its own, and the process in no session keeps its own
- **AND** the alert says "An AI agent's /bin/cat tried to read your SSH private key", and a kind from a newer agent reads "a credential file"

#### Scenario: A report is the generation it names

- **GIVEN** a report of pid 6991's generation running cat, and pid 6991's earlier generation, the shell that execed cat, in another session
- **WHEN** the report is evaluated
- **THEN** the alert is cat's, in cat's session
- **AND** a report naming a generation not yet recorded is retried rather than resolved to another, and a session id at the marker's 256-byte limit still yields a subject the alert column holds

#### Scenario: The agent's own read joins the session it leads

- **GIVEN** an agent process with no session marker, whose commands record its pid as their session's agent
- **WHEN** it reads a credential
- **THEN** its read is grouped under the latest session first seen during the process's lifetime and at or before the read, so it becomes evidence on that session's alert
- **AND** a session of an earlier process that had the same pid, or one first seen after the read, is not chosen
