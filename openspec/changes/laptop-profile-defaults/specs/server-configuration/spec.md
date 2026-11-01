## ADDED Requirements

### Requirement: The deployment profile is a setting

The server SHALL take its deployment profile from `EDR_PROFILE`, `server` by default or `laptop` in any case, SHALL refuse to start on any other value, and SHALL report the profile in the session probe. The laptop compose profile SHALL set it to `laptop`.

#### Scenario: The profile defaults to server and accepts laptop

- **GIVEN** a server configured without `EDR_PROFILE`, with `Laptop`, or with `desktop`
- **WHEN** it starts
- **THEN** its profile is `server`, `laptop`, or it refuses to start naming the two values, respectively

#### Scenario: The session probe reports the profile

- **GIVEN** a signed-in operator on a server whose profile is unset, or `laptop`
- **WHEN** the console reads `GET /api/session`
- **THEN** the response's `profile` is `server`, or `laptop`

#### Scenario: The laptop stack runs the laptop profile

- **GIVEN** the laptop compose profile
- **WHEN** the server's environment is read
- **THEN** `EDR_PROFILE` is `laptop`
