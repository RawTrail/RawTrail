## ADDED Requirements

### Requirement: Set-credential-guard command

The agent SHALL forward a `set_credential_guard` command's payload to the security extension as `credential_guard.update`, byte for byte, after checking only its envelope: a version that is not negative, a mode, and a credentials array. Which modes and credentials apply is the extension's to decide. The command SHALL complete with the version, mode and credential count, and SHALL fail with the reason when the envelope is invalid or the send fails.

#### Scenario: A credential guard policy is forwarded

- **GIVEN** a `set_credential_guard` command at version 0 or later, with a mode the agent may not know
- **WHEN** the agent executes it
- **THEN** the extension is sent the payload unchanged
- **AND** the command completes with its version, mode and credential count

#### Scenario: A credential guard payload is invalid

- **GIVEN** a `set_credential_guard` command that is not JSON, has a negative version, no mode, or credentials that are not an array
- **WHEN** the agent executes it
- **THEN** the command fails with the reason and nothing reaches the extension
