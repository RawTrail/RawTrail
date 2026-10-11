## ADDED Requirements

### Requirement: Revoke ends prior credentials

Revoking a host's enrollment SHALL invalidate every host token issued to that host before the revoke, including after the host re-enrolls. The server SHALL achieve this by bumping the host's token epoch in the same write that marks it revoked. The revoke's audit entry SHALL commit in the same transaction as the revoke. A revoke SHALL NOT prevent an agent holding the enroll secret from re-enrolling.

#### Scenario: Stolen token stays rejected after re-enrollment

- **GIVEN** an enrolled host whose token has been copied
- **WHEN** an operator revokes the host and its agent re-enrolls with the enroll secret
- **THEN** the copied token is rejected by every authenticated route and by the refresh route
- **AND** the token the re-enrolled agent received verifies and refreshes

#### Scenario: Revoke audit entry commits with the revoke

- **GIVEN** an authenticated operator revoking a host
- **WHEN** the revoke commits
- **THEN** an `enrollment.revoke` audit entry has committed with it and is delivered as an audit row naming the acting principal, the request's address, the host, and the operator-supplied actor and reason
- **AND** a revoke whose audit entry cannot be written is rolled back, leaving the host's enrollment and token epoch unchanged
