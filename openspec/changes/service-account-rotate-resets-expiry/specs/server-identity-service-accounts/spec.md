## ADDED Requirements

### Requirement: Rotating a service account resets its lifetime

The system SHALL give a rotated service account a fresh expiry from the moment of rotation, 90 days by default or a requested number of days capped at 365, and SHALL return the account's new expiry with the new secret. It SHALL refuse to rotate a revoked or an expired account, because no secret issued to one can mint a token.

#### Scenario: Rotation sets a fresh expiry

- **GIVEN** an active service account with three days left
- **WHEN** an admin rotates it without naming a lifetime
- **THEN** the account expires 90 days from the rotation and the response carries that expiry with the new secret
- **AND** the new secret mints a token

#### Scenario: A requested lifetime is honoured and capped

- **GIVEN** an active service account
- **WHEN** an admin rotates it asking for 30 days, or for more than 365
- **THEN** the account expires 30 days from the rotation, or 365 for the larger request

#### Scenario: A revoked or expired account is not rotated

- **GIVEN** a service account that is revoked, or whose expiry has passed
- **WHEN** an admin rotates it
- **THEN** the request is refused with a conflict
- **AND** the account's secret, epoch and expiry are unchanged
