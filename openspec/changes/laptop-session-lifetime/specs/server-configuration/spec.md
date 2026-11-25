## ADDED Requirements

### Requirement: A laptop's break-glass sessions last days

On a laptop install, the server SHALL default the break-glass session idle timeout to 7 days and its absolute timeout to 30 days when `EDR_BREAKGLASS_SESSION_IDLE_TIMEOUT` or `EDR_BREAKGLASS_SESSION_ABSOLUTE_TIMEOUT` is unset. A value an operator sets SHALL win, and a server install SHALL keep 15 minutes and an hour. The reauth window SHALL be unchanged.

#### Scenario: A laptop's owner stays signed in

- **GIVEN** a laptop install with neither break-glass session timeout set
- **WHEN** the server loads its configuration and the owner signs in
- **THEN** the session's idle timeout is 7 days and it expires 30 days after sign-in

#### Scenario: A set lifetime and a server keep theirs

- **GIVEN** a laptop install with the break-glass timeouts set to 2 hours and 12 hours, and a server install with neither set
- **WHEN** each loads its configuration
- **THEN** the laptop uses 2 hours and 12 hours, and the server uses 15 minutes and an hour
