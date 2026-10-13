## ADDED Requirements

### Requirement: Story root and tool call stamping

The graph builder SHALL set `story_root_id` to a row's own id when its fork-time parent is launchd, or when it is first seen at exec under launchd without being reparented; otherwise to its creator's, or on a re-exec the prior generation's. An exec whose creator's pid equals its `CLAUDE_PID` marker SHALL set `tool_call_group` to its process group, and every other row SHALL inherit it as it inherits the agent session.

#### Scenario: A story reaches the process launchd started

- **GIVEN** launchd started a process whose descendants include an agent and its tool calls
- **WHEN** the events are processed, in one batch or one at a time
- **THEN** every descendant's story root is that process's row, including a daemon launchd later adopted

#### Scenario: A process launchd started is its own story root

- **GIVEN** a process forked by launchd, and a process first seen at exec under launchd that was not reparented
- **WHEN** the events are processed
- **THEN** each is its own story root
- **AND** a reparented process first seen at exec has none

#### Scenario: A tool call is keyed by the agent child's process group

- **GIVEN** the agent process starts a tool call whose exec carries `CLAUDE_PID` naming the agent
- **WHEN** the events are processed
- **THEN** the tool call's row carries its own process group as its tool call
- **AND** the agent process and its helpers carry none

#### Scenario: A tool call survives daemonizing and setsid

- **GIVEN** a tool call double-forks a daemon, and starts a process that calls `setsid` into a new process group
- **WHEN** the events are processed
- **THEN** both carry the tool call's group
