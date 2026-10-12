## ADDED Requirements

### Requirement: Shutdown exports the final gauge values

On a graceful stop, the server SHALL flush its OTel telemetry before it closes its data store connections, so the final collection of every observable gauge can still query the data store.

#### Scenario: The final collection reaches the data store

- **GIVEN** a running server with OTLP export enabled
- **WHEN** the server stops gracefully
- **THEN** the OTel flush runs while the data store connections are still open
- **AND** the data store connections are closed after the flush

### Requirement: Ingest disconnects are not errors

The ingest handler SHALL log a failed store write at info level, and write no response, when the failure is a cancellation caused by the agent's request ending. Every other failed write, including a cancellation while the request is still live, SHALL be logged at error level, and a failed event-archive or work-queue write SHALL be answered with HTTP 5xx.

#### Scenario: A disconnect is info-logged

- **GIVEN** an agent uploading a batch
- **WHEN** the agent disconnects and a store write fails as cancelled
- **THEN** the failure is logged at info level, not error level, and no response is written

#### Scenario: A live cancellation stays an error

- **GIVEN** an agent whose upload request is still live
- **WHEN** an event-archive or work-queue write fails as cancelled
- **THEN** the failure is logged at error level and the handler responds with HTTP 5xx

#### Scenario: A write failure stays an error

- **GIVEN** an agent uploading a batch
- **WHEN** an event-archive or work-queue write fails with an error that is not a cancellation
- **THEN** the failure is logged at error level and the handler responds with HTTP 5xx
