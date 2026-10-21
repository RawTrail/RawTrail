## ADDED Requirements

### Requirement: Agent session markers capture

The `exec` event SHALL carry, in an `env` object, the exec'd process's environment variables whose full name is on the agent session marker allowlist. Names SHALL be matched in full, never by prefix, and no other variable SHALL be sent. A value longer than 256 UTF-8 bytes SHALL be dropped. The `env` key SHALL be omitted when no marker is present.

#### Scenario: An agent-spawned process carries its session

- **GIVEN** the endpoint event capture is running
- **WHEN** a Claude Code session runs a command
- **THEN** the command's `exec` events carry `env.CLAUDE_CODE_SESSION_ID`
- **AND** its value equals the name of the session's transcript file

#### Scenario: A credential never leaves the host

- **GIVEN** a process whose environment holds `CLAUDE_CODE_MESSAGING_TOKEN` beside the session markers
- **WHEN** it execs
- **THEN** the `exec` event carries the markers
- **AND** neither the token's name nor its value appears in the event

#### Scenario: An oversized marker is dropped

- **GIVEN** a process whose environment holds an allowlisted marker longer than 256 UTF-8 bytes
- **WHEN** it execs
- **THEN** the `exec` event omits that marker rather than truncating it

#### Scenario: A process without markers keeps its shape

- **GIVEN** a process whose environment holds no allowlisted marker
- **WHEN** it execs
- **THEN** the `exec` event has no `env` key
- **AND** a payload that carries `env` still decodes on a server that does not read it
