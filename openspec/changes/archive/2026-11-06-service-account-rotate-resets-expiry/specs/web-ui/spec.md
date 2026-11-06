## ADDED Requirements

### Requirement: The service accounts page shows when each credential expires

The service accounts page SHALL show each account's expiry as a date with the time remaining, and SHALL flag an active account expiring within 14 days. The one-time secret card SHALL state the expiry of the credential just issued. The page SHALL offer Rotate only on an active account.

#### Scenario: The list shows expiry and flags a near one

- **GIVEN** an active service account expiring in 5 days
- **WHEN** an operator opens the service accounts page
- **THEN** the account's row shows its expiry date and the time remaining
- **AND** the row is flagged as expiring soon

#### Scenario: The secret card states the expiry

- **GIVEN** an operator who creates or rotates a service account
- **WHEN** the one-time secret card appears
- **THEN** it states when the credential expires

#### Scenario: A dead account offers no rotate

- **GIVEN** a revoked or an expired service account
- **WHEN** an operator views its row
- **THEN** the row offers no Rotate action
