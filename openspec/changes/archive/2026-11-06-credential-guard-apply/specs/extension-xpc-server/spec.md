## ADDED Requirements

### Requirement: Inbound credential guard update

The security extension SHALL pass a `credential_guard.update` message's data to the credential guard, and SHALL ignore one with no data, leaving the policy in force.

#### Scenario: The agent pushes a credential guard policy

- **GIVEN** a connected agent
- **WHEN** it sends `credential_guard.update` with a policy
- **THEN** the policy's bytes reach the credential guard

#### Scenario: A credential guard update with no data is rejected

- **GIVEN** a connected agent
- **WHEN** it sends `credential_guard.update` with no data or empty data
- **THEN** the update is rejected and the policy in force is unchanged
