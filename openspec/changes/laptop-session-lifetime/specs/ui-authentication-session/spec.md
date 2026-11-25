## MODIFIED Requirements

### Requirement: Sessions expire on idle and absolute timeouts per class

The system SHALL bound every session by an idle timeout from its last activity and an absolute timeout from its creation, pinned on the session row at mint, chosen by class: 8 hours and 24 hours for an SSO session, and for a break-glass session 15 minutes and 1 hour on a server install, 7 days and 30 days on a laptop. The cookie SHALL expire with the absolute cap. Activity SHALL slide the idle timeout, never past the absolute cap, and a request past either SHALL be rejected with 401.

#### Scenario: Cookie carries the absolute timeout on login

- **GIVEN** the operator logs in successfully
- **WHEN** the response sets the session cookie
- **THEN** the cookie's `Expires` attribute equals the session's absolute expiry (creation time plus the class's absolute timeout)
- **AND** the cookie's `Max-Age` attribute is the number of seconds remaining until that expiry

#### Scenario: A request after the absolute cap is rejected

- **GIVEN** a session whose absolute cap has elapsed
- **WHEN** the client uses that session cookie on any session-required endpoint
- **THEN** the server treats the session as expired and returns 401
- **AND** the request is not authenticated as the original operator

#### Scenario: A request after the idle window is rejected

- **GIVEN** a session that is still within its absolute cap but whose last activity is older than the class's idle timeout
- **WHEN** the client uses that session cookie on any session-required endpoint
- **THEN** the server treats the session as expired and returns 401

#### Scenario: Activity within the idle window slides the session

- **GIVEN** a session whose operator keeps interacting within the idle window
- **WHEN** each request advances the session's last-activity time
- **THEN** the session stays valid past what would otherwise be the idle-timeout cutoff
- **AND** the session is still rejected once the absolute cap elapses, regardless of activity

#### Scenario: Break-glass sessions use the strict timeout pair

- **GIVEN** a session minted through the break-glass surface (`auth_method='local_password'`)
- **WHEN** its idle gate is evaluated
- **THEN** the strict break-glass pair applies (15-minute idle, 1-hour absolute), not the normal pair
- **AND** on a laptop install, where break-glass is the owner's only sign-in, the pair is 7 days idle and 30 days absolute
