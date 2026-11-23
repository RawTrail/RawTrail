## ADDED Requirements

### Requirement: A graceful stop settles the batch in progress

When the server stops gracefully while the processor holds a claimed batch, the processor SHALL hand that batch back to the queue, or acknowledge it if its evaluation completed, before the stop finishes. A stop SHALL NOT leave a batch claimed for its lease, since a claim stops at its host's oldest in-flight event and the host's later events would wait the lease out. The claim lease SHALL remain the fallback for a crash.

#### Scenario: A batch stopped during evaluation is handed back

- **GIVEN** a processor evaluating a claimed batch
- **WHEN** the server stops and the evaluation fails because of it
- **THEN** the batch's events are returned to the queue and none is left claimed

#### Scenario: A batch stopped while its graph is built is handed back

- **GIVEN** a processor folding a claimed batch into the process graph
- **WHEN** the server stops and the fold fails because of it
- **THEN** the batch's events are returned to the queue and none is left claimed

#### Scenario: A batch evaluated as the stop arrives is acknowledged

- **GIVEN** a processor whose evaluation of a claimed batch completes after the server began stopping
- **WHEN** it acknowledges the batch
- **THEN** the acknowledgement lands and the batch's events are marked processed rather than left claimed

#### Scenario: The backlog resumes after a restart without waiting for the lease

- **GIVEN** a server stopped while a host's batch was in progress, with more of that host's events queued behind it
- **WHEN** a processor starts again
- **THEN** it processes that host's events, starting with the batch that was handed back, without waiting for the claim lease to expire
