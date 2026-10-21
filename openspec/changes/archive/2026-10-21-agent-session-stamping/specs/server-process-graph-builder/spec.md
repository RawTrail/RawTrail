## ADDED Requirements

### Requirement: Agent session stamping

The graph builder SHALL stamp `agent_session_id`, `agent_name` and `agent_version` on the row an exec images when the exec carries a `CLAUDE_CODE_SESSION_ID` marker of at most 256 bytes, taking the name and version from `AI_AGENT`. Every other row SHALL take its creator's session on fork, keep it through an exec without the marker, and keep the prior generation's on a re-exec. A row with no creator and no marker SHALL have none.

#### Scenario: A tool call stamps its session

- **GIVEN** an agent process without the marker runs a tool call whose shell carries `CLAUDE_CODE_SESSION_ID` and `AI_AGENT`
- **WHEN** the events are processed
- **THEN** the shell's row carries the session, the agent name and the version
- **AND** the agent process's own row carries none

#### Scenario: A child stripped of its environment keeps the session

- **GIVEN** a tool call's shell runs `env -i` or `sudo`, whose exec carries no markers
- **WHEN** the events are processed
- **THEN** the child's row carries the shell's session

#### Scenario: A daemonized descendant keeps the session

- **GIVEN** a tool call double-forks a process that launchd adopts before it execs
- **WHEN** the events are processed, in one batch or one at a time
- **THEN** the daemon's row carries the tool call's session

#### Scenario: A new marker starts a new session

- **GIVEN** a process in one session execs with a different `CLAUDE_CODE_SESSION_ID`, as after `/clear`
- **WHEN** the events are processed
- **THEN** its row and the rows it creates carry the new session

#### Scenario: A process outside any agent has no session

- **GIVEN** a process whose creator has no session, including an agent helper that carries `AI_AGENT` but no session marker
- **WHEN** the events are processed
- **THEN** its row carries no session
- **AND** a session marker over 256 bytes is ignored
