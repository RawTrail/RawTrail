## ADDED Requirements

### Requirement: Break-glass WebAuthn challenges are single-use

The server SHALL accept each break-glass WebAuthn sign-in or reauth challenge once: finishing a ceremony SHALL record the challenge in MySQL, and a challenge already recorded SHALL be refused with 401 and the audit reason `webauthn.challenge_reused`, on every replica. A record SHALL be deleted once its challenge has expired.

#### Scenario: A repeated challenge is refused

- **GIVEN** a passkey that reports a sign count of 0, and a challenge cookie and assertion that signed in once, by the passkey-only or the password sign-in
- **WHEN** the same challenge is submitted again before it expires
- **THEN** it is refused as reused, no session is minted, and a fresh challenge still signs in

#### Scenario: Expired challenge records are swept

- **GIVEN** recorded challenges, one past its expiry and one not
- **WHEN** the identity cleanup runs
- **THEN** only the expired record is deleted
