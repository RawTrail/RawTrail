## ADDED Requirements

### Requirement: Agent session commands

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/commands` SHALL list an AI agent session's commands in the order they started, each with its label, its processes, what launchd started for it, and the Bash call it pairs with, paged by an opaque cursor with the matched total. `q` SHALL keep the commands whose label, process path or command line holds it. It SHALL say when a read was cut, require process read, reject a malformed cursor and answer 404 for an unknown session.

#### Scenario: Commands are labelled and paired with their tool calls

- **GIVEN** a Claude Code session whose Bash calls ran a pipeline, a backgrounded subshell, `nohup`, `env -i` and a nested `sh -c`, each in the agent's wrapper shell, with the calls the agent recorded
- **WHEN** its commands are read
- **THEN** there is one command per call, in the order they ran, labelled with the command the agent ran however it was quoted
- **AND** each holds every process it started, a daemon it backgrounded included
- **AND** each is paired with the nearest earlier call that ran it, its owner's only, and identical calls of one turn are marked ambiguous rather than guessed

#### Scenario: Commands page in the order they started

- **GIVEN** a session with more commands than one page
- **WHEN** its pages are read in turn by their cursors
- **THEN** every command is listed once, in the order they started, each page with the matched total
- **AND** the last page names no next cursor, and a malformed cursor is rejected with `invalid_cursor`

#### Scenario: A query keeps the commands it finds

- **GIVEN** a session's commands
- **WHEN** they are read with `q`
- **THEN** only the commands whose label, or whose processes' path or command line, holds it are listed, regardless of case
- **AND** the wrapper shell's script is not searched apart from the command it ran

#### Scenario: What launchd started is listed under the command that asked for it

- **GIVEN** a command that ran `open -a TextEdit`, after which launchd started TextEdit and TextEdit's service
- **WHEN** its commands are read
- **THEN** TextEdit is listed under `open` and named an inferred launch, and the service under TextEdit and named a service

### Requirement: Agent session summary command total

The agent session summary SHALL count the session's commands as `GET /api/hosts/{host_id}/agent-sessions/{session_id}/commands` lists them, over the same process read.

#### Scenario: The summary counts what the commands list lists

- **GIVEN** a session whose processes are two commands
- **WHEN** its summary and its commands are read
- **THEN** the summary counts two commands, the commands list's total
