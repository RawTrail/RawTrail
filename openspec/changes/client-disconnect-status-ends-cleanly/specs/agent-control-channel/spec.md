## ADDED Requirements

### Requirement: A status failure after a disconnect ends cleanly

The system SHALL end the control RPC with no error when a failed receive carries a gRPC status of Unavailable or Canceled, which is how net/http's HTTP/2 server reports a client that went away. It SHALL end the RPC with that status when the status carries any other code, even when the RPC context has been cancelled, because gRPC cancels the context moments after it rejects a frame. The decision SHALL rest on the status code, never on its text or on the context.

#### Scenario: An Unavailable status is a disconnect

- **GIVEN** a host holding an open control connection
- **WHEN** the client's socket dies and the receive fails with a gRPC status of Unavailable
- **THEN** the RPC ends with no error, whether or not the RPC context has been cancelled yet, and the span is recorded as successful

#### Scenario: A rejected frame is a fault

- **GIVEN** a host holding an open control connection
- **WHEN** the client sends a frame the server cannot read, or one over the size limit
- **THEN** the RPC ends with the status that rejected it, even though the RPC context is cancelled by then
- **AND** an unreadable frame is recorded as a failed connection
