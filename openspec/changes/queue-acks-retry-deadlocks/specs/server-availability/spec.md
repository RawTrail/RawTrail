## ADDED Requirements

### Requirement: Queue acknowledgements survive lock contention

Acknowledging a processed batch, and returning one that failed, SHALL retry a database deadlock with bounded backoff, as claiming and pruning do, so a batch that processed is not left in flight for its claim lease, holding its host's later events, because its acknowledgement lost a lock race.

#### Scenario: An ack that loses a deadlock is retried

- **GIVEN** a claimed batch whose acknowledgement deadlocks with a concurrent transaction and is chosen as the victim
- **WHEN** the acknowledgement runs
- **THEN** it is retried and succeeds, and the whole batch is marked processed
- **AND** a batch returned as failed under the same contention is retried the same way
