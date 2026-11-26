## ADDED Requirements

### Requirement: A rejected frame outranks the failures it causes

The system SHALL end the control RPC with the status that rejected a client's frame even when a teardown of that connection was recorded at the same time, such as a send that failed on the rejected stream, because gRPC has already sent that status to the client. The recorded span SHALL name the status the client received.

#### Scenario: A heartbeat in flight does not replace the rejection

- **GIVEN** a connected host with a heartbeat being sent to it
- **WHEN** the client's frame is rejected for its size and the heartbeat then fails on the ended stream
- **THEN** the RPC ends with `ResourceExhausted`, the status the client received, not the outbound failure

#### Scenario: A revocation recorded with the rejection does not replace it

- **GIVEN** a connected host whose receive loop has already ended with a rejection of its frame
- **WHEN** a revocation of its token is recorded at the same moment, while the client is still attached
- **THEN** the RPC ends with the rejection's status, not the revocation's `Unavailable`
