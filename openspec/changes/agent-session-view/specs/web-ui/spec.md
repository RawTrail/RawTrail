## ADDED Requirements

### Requirement: AI agent session view

The UI SHALL offer a view of one AI agent session listing its processes in the order its tool calls ran, one group per tool call. Each group SHALL be labelled with the command the agent ran and SHALL contain every process the call started, including a daemon it backgrounded. The shells that run a call SHALL be hidden unless the operator asks to see them. Each process SHALL open in its host's process graph.

#### Scenario: Tool calls are labelled with the command the agent ran

- **GIVEN** a Claude Code session whose Bash tool calls ran `ls /private/tmp | head -3`, `(sleep 47 &)`, `nohup sleep 48 >/dev/null 2>&1 &`, `env -i /bin/echo qa1280` and `/bin/sh -c "sleep 1; echo qa1280-done"`, each in Claude Code's `zsh -c` wrapper script
- **WHEN** the operator opens the session's view
- **THEN** it lists five groups in that order, each labelled with that command as the agent wrote it, quotes included

#### Scenario: A daemon the agent started stays in its tool call

- **GIVEN** a tool call that backgrounded a process from a subshell, so the process outlived the call and was reparented to launchd
- **WHEN** the operator opens the session's view
- **THEN** the backgrounded process is listed inside that tool call's group, not as a separate group or outside the session

#### Scenario: Shell plumbing is hidden unless verbose

- **GIVEN** a tool call run in the agent's wrapper shell, whose subshell forked a copy of that shell
- **WHEN** the operator opens the session's view
- **THEN** the group lists the commands the call ran, without the wrapper shell or the shell copy
- **AND** turning on "Show shell plumbing" lists them in their place
