## ADDED Requirements

### Requirement: The session probe refuses a service-account token

The session endpoint SHALL answer a request authenticated by a valid service-account bearer token with 403 and the error code `session_required`, because that credential carries no browser session. It SHALL NOT answer such a request with a server error.

#### Scenario: Service-account token on the session probe

- **GIVEN** a caller holds a valid service-account access token
- **WHEN** it issues a GET to the session endpoint with that token as a bearer credential
- **THEN** the server returns 403 with the error code `session_required`
- **AND** no session, CSRF token or permission set is returned
