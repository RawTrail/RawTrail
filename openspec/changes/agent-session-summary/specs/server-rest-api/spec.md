## ADDED Requirements

### Requirement: Agent session summary

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/summary` SHALL total one AI agent session without listing it: retained processes, alerts by severity without monitor records, tool calls by tool, each once with copies of another session's left out, and file events by operation with destination and DNS name totals over the scope the session's activity rollup reads, saying how far it reached. It SHALL require process read and answer 404 for an unknown session.

#### Scenario: A session is totalled

- **GIVEN** a session with processes, alerts of two severities and a monitor record raised on them, and a process of another session
- **AND** tool calls the agent recorded, one of them reported twice and one copied from the session it was forked from
- **AND** file events of several operations, one coalesced, and connections and lookups by its processes
- **WHEN** its summary is read
- **THEN** it counts the session's processes, and its alerts by severity without the monitor record
- **AND** each tool call once, by tool, without the copied call
- **AND** the file events by operation, the coalesced one counting as the operations it stands for, with the activity totals its rollup reports

#### Scenario: The summary reports the reach of its activity

- **GIVEN** a session whose processes outnumber the generations the activity rollup reads
- **WHEN** its summary is read
- **THEN** the activity is counted over the generations the rollup reads, and the summary reports that the scope was truncated

#### Scenario: An unknown session has no summary

- **GIVEN** a host that holds no session with the requested id
- **WHEN** its summary is read
- **THEN** the request is answered 404 and nothing is counted
