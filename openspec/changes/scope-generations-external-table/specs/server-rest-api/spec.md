## ADDED Requirements

### Requirement: Agent session activity scope travels beside the query

An AI agent session's activity reads SHALL send the process generations of their scope to the event archive beside the query, not in its text, so a read's query stays the same size however many processes the session holds. What a read returns SHALL not change.

#### Scenario: A large scope reads under the default query size

- **GIVEN** an activity scope of 20,000 process generations, more than fit in ClickHouse's default query size as text
- **WHEN** the scope's totals and rollup are read with the default query size
- **THEN** both reads succeed
- **AND** they count the events of the scope's last generation
