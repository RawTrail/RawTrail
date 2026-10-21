## ADDED Requirements

### Requirement: Heartbeat freshness bumps survive lock contention

The system SHALL retry a heartbeat freshness update that the database aborts as a deadlock victim, up to a bounded number of attempts, so ordinary contention with concurrent writes to the process records neither loses the update nor reports it as an error. An update still aborted after the last attempt is reported as an error.

#### Scenario: A bump that loses a deadlock is retried and applied

- **GIVEN** live snapshot-originated process records for PIDs P1 and P2 on a host
- **AND** a concurrent transaction that holds P2's record and then requests P1's while a heartbeat bump for both PIDs holds P1's and waits on P2's
- **WHEN** the database aborts the bump as the deadlock victim
- **THEN** the concurrent transaction commits
- **AND** the bump is retried and both records' freshness timestamps carry the heartbeat's timestamp
