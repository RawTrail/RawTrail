## ADDED Requirements

### Requirement: A graceful stop settles the batch in progress

When the server stops gracefully while the processor holds a claimed batch, the processor SHALL hand the batch back, or acknowledge it if its evaluation completed, and record its monitor-mode counts, before the stop finishes. A stop SHALL NOT leave a batch claimed for its lease. A requeue that needs the host's lock SHALL NOT run once that lock is lost. The claim lease SHALL remain the fallback for a crash.

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

#### Scenario: A settled batch's monitor counts are recorded

- **GIVEN** a stop that arrives as a batch with monitor-mode matches is acknowledged, or withdrawn after repeated failure
- **WHEN** the batch is settled
- **THEN** its monitor-mode counts are recorded, since the batch is never evaluated again and they would otherwise be lost with the stop

#### Scenario: A requeue does not outlive a lost host lock

- **GIVEN** a processor folding a claimed batch under its host's lock
- **WHEN** the lock is lost and the fold fails
- **THEN** the batch is not handed back without the lock, since its row locks would let another claimer fold the host's later events first, and the claim lease returns it instead
