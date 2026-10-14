## ADDED Requirements

### Requirement: Alerts by agent session

`GET /api/alerts` SHALL accept `agent_session` with `host_id` and return only the alerts raised on that AI agent session's processes on that host, with the list's other filters still applied. A request with `agent_session` and no `host_id` SHALL be rejected, since a session id names a session only on its host.

#### Scenario: A session's alerts are listed

- **GIVEN** alerts on a session's processes, a monitor record on one of them, an alert on another session's process, and an alert on a process of the same session id on another host
- **WHEN** the alerts are listed with the host and the session
- **THEN** only the session's alerts on that host are returned
- **AND** asking for monitor records returns the monitor record instead

#### Scenario: A session filter without its host is rejected

- **GIVEN** a request for alerts with `agent_session` and no `host_id`
- **WHEN** it is made
- **THEN** it is rejected with `agent_session_requires_host_id` and the alerts are not read
