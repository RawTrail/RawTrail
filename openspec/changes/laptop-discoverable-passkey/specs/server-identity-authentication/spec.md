## ADDED Requirements

### Requirement: A laptop's passkey sign-in can name no account

On a laptop install, the passkey-only sign-in SHALL accept a challenge requested with no email, which names no account, and SHALL then sign in the break-glass account the passkey's user handle names, refusing a handle that names no break-glass account. The password sign-in SHALL NOT name its account by user handle. A laptop install's setup SHALL ask the authenticator for a discoverable credential, and a server install's SHALL NOT.

#### Scenario: A discoverable passkey signs its owner in

- **GIVEN** a laptop install whose owner registered a discoverable passkey, and a browser that knows no email
- **WHEN** the browser requests the passkey-only challenge with no email, and returns that passkey's assertion, which verified the owner
- **THEN** the challenge listed no credentials, named no account and required user verification
- **AND** a session is minted for the account the user handle names, the sign count is recorded, and the audit row's method is `passkey_only`

#### Scenario: A handle outside break-glass is refused

- **GIVEN** a challenge that named no account
- **WHEN** the assertion's user handle names an account outside break-glass, or no account
- **THEN** it is refused with 401 and no session is minted, and the audit row's reason is `not_breakglass` or `user_not_found`

#### Scenario: Laptop setup asks for a discoverable credential

- **GIVEN** a valid setup token
- **WHEN** setup begins on a laptop install
- **THEN** the registration options ask for a resident key as preferred, so an authenticator that cannot store one still registers
- **AND** on a server install they ask for none
