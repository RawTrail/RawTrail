## ADDED Requirements

### Requirement: Ingest drains and resolves clients like the server

The standalone ingest service SHALL resolve a client's address from `X-Forwarded-For` only when the connecting peer is a configured trusted proxy, as the server does. On SIGTERM it SHALL report not-ready on its readiness probe and keep serving for the configured drain window before it closes its listener, as the server does.

#### Scenario: A forwarded address is believed only from a trusted proxy

- **GIVEN** an HTTP service configured to trust one proxy network
- **WHEN** requests carrying `X-Forwarded-For` arrive from a peer inside that network, from a peer outside it, and with no trusted proxies configured
- **THEN** only the request from the trusted peer is attributed to the forwarded address, and the others to their peer address

#### Scenario: Ingest readiness reports not-ready once draining begins

- **GIVEN** a running ingest service whose readiness probe reports ready
- **WHEN** it begins draining on SIGTERM
- **THEN** its readiness probe reports not-ready with HTTP 503
