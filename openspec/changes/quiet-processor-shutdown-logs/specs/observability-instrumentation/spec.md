## ADDED Requirements

### Requirement: Processor shutdown is not logged as an error

The detection processor SHALL log a failed queue, lock or store call at debug level when the failure is a cancellation caused by its own worker context ending, as on server shutdown. Every other failure, including a cancellation while the worker context is still live, SHALL be logged at error level.

#### Scenario: A query cancelled by shutdown is debug-logged

- **GIVEN** a detection worker with a queue query in flight
- **WHEN** the server stops, cancelling the worker's context and the query fails as cancelled
- **THEN** the failure is logged at debug level, not error level

#### Scenario: A cancellation while the worker is live stays an error

- **GIVEN** a detection worker whose context is still live
- **WHEN** a queue query fails as cancelled, as when a lost host lock cancels the locked section
- **THEN** the failure is logged at error level

#### Scenario: A real failure during shutdown stays an error

- **GIVEN** a detection worker whose context has been cancelled
- **WHEN** a queue query fails with an error that is not a cancellation
- **THEN** the failure is logged at error level
