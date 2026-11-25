## ADDED Requirements

### Requirement: The sign-in options report the profile

The server SHALL answer `GET /api/auth/options` without a session with the deployment profile, `server` or `laptop`, and nothing else, so the login page can choose its sign-in controls before anyone has signed in.

#### Scenario: The login page reads the profile before sign-in

- **GIVEN** a server whose profile is unset, or `laptop`
- **WHEN** a browser with no session reads `GET /api/auth/options`
- **THEN** the response is 200 with `profile` `server`, or `laptop`, and no other field
