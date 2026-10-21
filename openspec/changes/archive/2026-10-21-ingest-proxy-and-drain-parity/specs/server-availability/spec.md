## ADDED Requirements

### Requirement: Ingest drains and resolves clients like the server

The standalone ingest service SHALL believe a client's `X-Forwarded-For` address only when the peer is a configured trusted proxy, as the server does, and both SHALL record that resolved address as the request span's client address. On SIGTERM the ingest service SHALL report not-ready and keep serving for the configured drain window before closing its listener. It SHALL emit HSTS whenever the client-facing connection is HTTPS.

#### Scenario: A forwarded address is believed only from a trusted proxy

- **GIVEN** an HTTP service configured to trust one proxy network
- **WHEN** requests carrying `X-Forwarded-For` arrive from a peer inside that network, from a peer outside it, and with no trusted proxies configured
- **THEN** only the request from the trusted peer is attributed to the forwarded address, and the others to their peer address

#### Scenario: Ingest readiness reports not-ready once draining begins

- **GIVEN** a running ingest service whose readiness probe reports ready
- **WHEN** it begins draining on SIGTERM
- **THEN** its readiness probe reports not-ready with HTTP 503

#### Scenario: A span records the resolved client address

- **GIVEN** a request carrying `X-Forwarded-For`
- **WHEN** its peer is a configured trusted proxy, is not, or no trusted proxies are configured
- **THEN** the request span's client address is the forwarded address only for the trusted proxy, and the peer address otherwise

#### Scenario: HSTS is emitted behind a TLS-terminating proxy

- **GIVEN** an ingest service configured with a TLS-terminating proxy in front of it
- **WHEN** it answers a request
- **THEN** the response carries a `Strict-Transport-Security` header
