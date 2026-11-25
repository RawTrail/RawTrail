## MODIFIED Requirements

### Requirement: Break-glass login lives at a separate path, not on the SSO login page

The system SHALL serve the break-glass login at `/admin/break-glass`. It MUST require both the local password and a successful WebAuthn assertion before minting a session, except that a laptop install's owner MAY sign in with a passkey alone that proved user verification. When an IP allowlist is configured for the break-glass path, a request from an IP not on it SHALL receive `404 Not Found`, so the path's existence is not acknowledged.

#### Scenario: Break-glass UI is reachable from an allowlisted IP

- **GIVEN** the requester's IP is on the configured allowlist (or no allowlist is configured)
- **WHEN** the requester issues `GET /admin/break-glass`
- **THEN** the server returns the break-glass login form (password + WebAuthn challenge)

#### Scenario: Off-allowlist requester receives 404

- **GIVEN** an IP allowlist is configured AND the requester's IP is not on it
- **WHEN** the requester issues any request to `/admin/break-glass` or `/admin/break-glass/setup`
- **THEN** the server returns `404 Not Found` with a body indistinguishable from a generic 404

#### Scenario: Successful break-glass login requires both password and WebAuthn

- **GIVEN** a requester who has supplied a correct break-glass password AND completed a WebAuthn assertion against a registered credential
- **WHEN** the server validates the submission
- **THEN** the server creates a session with `auth_method='local_password'` (with WebAuthn recorded as the second factor), sets the cookie with the break-glass tighter timeouts, and writes an audit row at WARN with action `auth.breakglass.success`

## ADDED Requirements

### Requirement: A laptop's owner signs in with their passkey alone

On a laptop install, the server SHALL offer a passkey-only break-glass sign-in whose challenge requires user verification, and SHALL mint a session for a valid assertion that proved it, with no password, auditing the sign-in as passkey-only. It SHALL refuse a challenge that did not require user verification. A server install SHALL NOT offer it.

#### Scenario: A verified passkey signs the owner in

- **GIVEN** a laptop install whose owner has a registered passkey
- **WHEN** the owner requests the passkey-only challenge and returns an assertion that verified them
- **THEN** the challenge required user verification, a session is minted and its cookie set with no password given, the sign count is recorded, and the audit row's method is `passkey_only`

#### Scenario: An unverified passkey is refused

- **GIVEN** a laptop install, and the password sign-in's challenge, which does not require user verification
- **WHEN** an assertion against it is sent to the passkey-only sign-in
- **THEN** it is refused with 401, no session is minted, nothing is recorded against the credential, and the audit row's reason is `webauthn.user_verification_required`

#### Scenario: A server install offers no passkey-only sign-in

- **GIVEN** a server install
- **WHEN** a requester posts to the passkey-only challenge or sign-in
- **THEN** neither route is mounted, so each is answered as any unknown path, and no challenge or session is issued
