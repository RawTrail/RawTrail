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

### Requirement: Host-token revocation is enforced per replica

The server SHALL enforce host-token revocation with a per-replica snapshot of revoked and epoch-bumped hosts, loaded before the replica serves and refreshed on a short interval. It SHALL reject a token whose host is revoked or whose epoch is below the host's current epoch. A replica SHALL refuse to start if the initial load fails, and SHALL keep its previous snapshot when a later refresh fails. Re-enrollment SHALL preserve the host's token epoch.

#### Scenario: Revoke invalidates after the snapshot refreshes

- **GIVEN** an enrolled host with a valid token
- **WHEN** an operator revokes the host, bumping its token epoch, and the snapshot refreshes
- **THEN** the host's pre-revoke token is rejected with 401
- **AND** a re-enrollment by that host mints a fresh token that verifies

#### Scenario: Re-enrollment preserves the token epoch

- **GIVEN** a host an operator has revoked, whose pre-revoke token is rejected
- **WHEN** the host re-enrolls
- **THEN** the freshly minted token verifies
- **AND** the pre-revoke token remains rejected, because the bumped epoch is preserved across the re-enrollment rather than reset

#### Scenario: Re-enrolled token verifies at once on the enrolling replica

- **GIVEN** a replica whose snapshot still shows a host as revoked
- **WHEN** the host re-enrolls through that replica
- **THEN** the new token verifies on that replica without waiting for a snapshot refresh
- **AND** every token below the host's epoch stays rejected

#### Scenario: Snapshot refresh failure retains the previous view

- **GIVEN** a populated revocation snapshot on a replica
- **WHEN** a subsequent refresh from the database fails
- **THEN** the replica continues enforcing the previously loaded revocation state
- **AND** does not fail open by dropping to an empty snapshot

## REMOVED Requirements

### Requirement: Revocation is enforced by a per-replica snapshot

**Reason**: Its credential-cycling scenario describes the removed `POST /api/enrollments/{host_id}/rotate`. A revoke now performs the epoch bump, and "Host-token revocation is enforced per replica" restates the same contract with scenarios that describe revoke.

**Migration**: None for operators of the snapshot itself. Credential cycling is a revoke. The tests' spec markers are repointed to the new requirement's scenarios in this change.
