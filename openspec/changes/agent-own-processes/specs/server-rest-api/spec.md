## ADDED Requirements

### Requirement: Agent own processes

A session's tree read SHALL return, as each agent node's `children`, the agent process's descendants that carry no session marker, forked while it served the session, without walking into a process stamped with a session, at most 2,000 per agent, and SHALL set `agent_own_truncated` when cut. `GET /api/alerts?agent_process=` SHALL select the alerts on them.

#### Scenario: A session read lists the agent's own processes under it

- **GIVEN** an agent that ran a plugin scan, a shell snapshot with a child and a fork daemon, a tool call with its child, a forked session's process under the daemon, and a scan after the span it served the session
- **WHEN** the session's tree is read
- **THEN** the agent's children are the scan, the snapshot with its child and the daemon, and not the tool call, the forked session's process or the later scan

#### Scenario: An agent's own processes' alerts are read by the agent

- **GIVEN** alerts on an agent's plugin scan, on its shell snapshot's child, on a later scan, and on the session's tool call
- **WHEN** the alerts are listed with `agent_process` and `host_id`, or with `agent_process` alone
- **THEN** the first three are returned and not the tool call's, and without `host_id` the request is rejected with `agent_process_requires_host_id`

