## ADDED Requirements

### Requirement: A disconnect that fails the receive with a status still ends the connection cleanly

The system SHALL end the control RPC with no error when a failed receive is followed within a short grace by the cancellation of the RPC's context, even when the failure is a gRPC status. The transport can fail the receive before it cancels the context, so the error's shape alone cannot tell a client that went away from one still attached.

#### Scenario: A status failure followed by the cancel is a disconnect

- **GIVEN** a host holding an open control connection
- **WHEN** the client's socket dies and the receive fails with a gRPC status before the RPC context is cancelled
- **THEN** the RPC ends with no error once the context is cancelled and the connection's span is recorded as successful

#### Scenario: A status failure on a live context is still a fault

- **GIVEN** a host holding an open control connection whose client stays attached
- **WHEN** the receive fails with a gRPC status and the RPC context stays live through the grace
- **THEN** the RPC ends with that status and the connection is recorded as failed
