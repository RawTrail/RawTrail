## ADDED Requirements

### Requirement: Agent sessions list bypass filter

`GET /api/agent-sessions` SHALL accept `bypass=true`, listing only the sessions the graph builder recorded as bypassed, with the filtered total and paging, and SHALL mark each row with `bypass`. Any other value of `bypass` SHALL be rejected with `invalid_bypass`.

#### Scenario: The list keeps the sessions that ran bypassed

- **GIVEN** a session that ran bypassed, a fork of it, and a session that never did
- **WHEN** the list is read with and without `bypass=true`
- **THEN** every row says whether its session ran bypassed, and the filtered list holds the two bypassed sessions across pages with a total of two
- **AND** `bypass=false` is rejected
