## ADDED Requirements

### Requirement: The credential guard is configured over the API

`GET /api/v1/detection-config/credential-guard` SHALL return the guard's mode (detect, protect or off) and protected credentials, each a known kind, a path below every home matched as a file or a name prefix, and owner tools by signing identity; version 0 is detect mode over the built-in credentials. `PUT` SHALL validate it, require credentials and a reason, refuse a stale `expected_version`, audit the previous and new policy, and queue `set_credential_guard` for every enrolled host.

#### Scenario: The guard detects over its built-in credentials until changed

- **GIVEN** a fleet whose credential guard no operator has changed
- **WHEN** its policy is read
- **THEN** it is version 0 in detect mode over the six built-in credentials, each marked default

#### Scenario: A change is stored, pushed and audited

- **GIVEN** three enrolled hosts
- **WHEN** an operator switches the guard to protect with a second SSH key prefix and another signed owner of the GitHub CLI token, giving a reason
- **THEN** the policy is stored as version 1, queued for the three hosts as set_credential_guard with its epoch, and audited with the reason, the previous mode and credentials and the host counts
- **AND** switching it off with no credentials is stored as version 2

#### Scenario: An invalid change is refused

- **GIVEN** the default policy
- **WHEN** a change omits credentials or the reason, names an unknown mode or kind, a path that is absolute, leaves the home or holds a control character, an owner with no identity or a malformed team, or one path twice, or its credentials encode to more than 8 KiB
- **THEN** it is refused with 400, and nothing is stored or queued

#### Scenario: A change to a policy that moved on is refused

- **GIVEN** a policy another operator changed after it was read
- **WHEN** a change names the version it was read at
- **THEN** it is refused with 409 and nothing is queued

#### Scenario: Reading and changing it need detection config permissions

- **GIVEN** an operator without detection_config.read or detection_config.write
- **WHEN** they read or change the policy
- **THEN** both are refused with 403 and nothing is changed

### Requirement: Hosts that miss the credential guard push get it

A periodic catch-up SHALL queue the current credential guard policy, the unchanged default included, for every enrolled host whose latest set_credential_guard does not carry its version and epoch, and SHALL leave a host whose copy is pending alone.

#### Scenario: A host is sent the policy it missed

- **GIVEN** a host enrolled before any change and one enrolled after a change it missed
- **WHEN** the catch-up runs before and after the change
- **THEN** both are sent the default first, and after the change only the late host is sent the policy, epoch included, as the push sent it
