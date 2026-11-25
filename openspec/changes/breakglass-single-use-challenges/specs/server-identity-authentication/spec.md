## ADDED Requirements

### Requirement: Break-glass WebAuthn challenges are single-use

The server SHALL accept each break-glass WebAuthn sign-in or reauth challenge once: finishing a ceremony SHALL record the challenge in MySQL, and a challenge already recorded SHALL be refused with 401 and the audit reason `webauthn.challenge_reused`, on every replica. A challenge with no expiry, or past it by the database's UTC clock, SHALL be refused and not recorded, and a record SHALL be deleted an hour after its challenge expired.

#### Scenario: A repeated challenge is refused

- **GIVEN** a passkey that reports a sign count of 0, and a challenge cookie and assertion that signed in once, by the passkey-only or the password sign-in
- **WHEN** the same challenge is submitted again before it expires
- **THEN** it is refused as reused, no session is minted, and a fresh challenge still signs in

#### Scenario: Expired challenge records are swept

- **GIVEN** recorded challenges expired two hours ago, expired a minute ago, and not yet expired
- **WHEN** the identity cleanup runs
- **THEN** only the record expired two hours ago is deleted: the hour's grace outlasts any consume still in flight for a challenge it judged unexpired

#### Scenario: An expired or unexpiring challenge is refused

- **GIVEN** a challenge with no expiry, as a cookie issued before the server enforced challenge timeouts carries, or one already past its expiry
- **WHEN** it finishes a sign-in
- **THEN** it is refused as expired and not recorded, so a replay that validated just before expiry cannot be recorded afresh after the sweep removed the original record
