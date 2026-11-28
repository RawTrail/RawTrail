## MODIFIED Requirements

### Requirement: Reauthentication is required for destructive actions

The system SHALL require a fresh authentication event within the reauth window (default 30 minutes, break-glass sessions included) before a destructive action: a host command, enrollment revocation, dismissing a `critical` alert, or a deployment-wide write to reachable addresses, rule content, detection tuning or application control. A stale session MUST get a typed error and an audit record. A service-account token is exempt; its role alone decides.

#### Scenario: Fresh session executes a destructive action

- **GIVEN** an authenticated session whose last fresh auth event is within the reauth window
- **WHEN** the operator invokes a destructive action that the policy would otherwise allow
- **THEN** the server proceeds with the action

#### Scenario: Stale session is challenged before destructive action

- **GIVEN** an authenticated session whose last fresh auth event is older than the reauth window
- **WHEN** the operator invokes a destructive action
- **THEN** the server returns a typed `reauth_required` error with the action and the reauth path
- **AND** an audit row is recorded with decision `deny` and reason `reauth_required`
- **AND** the action is not performed

#### Scenario: Fleet-wide change needs a fresh session

- **GIVEN** an admin session whose last fresh auth event is older than the reauth window
- **WHEN** the operator changes rule content, detection tuning (exclusions, per-rule mode or watched paths) or the reachable-address set, or revokes a host's enrollment
- **THEN** the server returns a typed `reauth_required` error and does not make the change
- **AND** the console prompts the operator to confirm their identity and then retries the change

#### Scenario: Application control needs a fresh session

- **GIVEN** an admin session whose last fresh auth event is older than the reauth window
- **WHEN** the operator creates, edits, bulk-adds or deletes an application control rule, or creates, edits or deletes a policy
- **THEN** the server returns a typed `reauth_required` error and does not make the change, because an allow rule or a deleted block rule lifts a block on every host the policy covers
- **AND** after the operator signs in again through the identity provider, an Add rule, Edit rule or Paste many dialog that was saving is open again with its unsaved work and a notice, and nothing was saved until they save again
- **AND** reading application control needs no fresh session
