## ADDED Requirements

### Requirement: The guard enforces the pushed policy

The extension SHALL enforce the newest credential guard policy pushed (by epoch, then version), persisted across restarts, and the built-in one until a push arrives. Its mode and credentials replace the built-ins: protect refuses what detect reports, off watches nothing. An open SHALL be decided by the most specific credential covering it, once that file is muted. An unknown mode SHALL turn the policy away; an unusable credential SHALL be skipped.

#### Scenario: A pushed policy replaces the built-in one

- **GIVEN** a pushed policy in protect mode over AWS credentials, a `.netrc` in a subdirectory and a second SSH key prefix with no owners
- **WHEN** an agent's process opens each
- **THEN** each open is refused and reported, and an SSH key the policy no longer lists is not guarded
- **AND** exactly those three credentials are muted in each home

#### Scenario: The most specific credential decides

- **GIVEN** a protect policy listing `.netrc` owned by curl and `work/.netrc` owned by no tool, and the SSH key prefix owned by OpenSSH and `.ssh/id_deploy` owned by no tool, in either order
- **WHEN** an agent's curl opens `work/.netrc` and its ssh opens `.ssh/id_deploy`
- **THEN** both opens are refused, while curl's `.netrc` and ssh's `.ssh/id_rsa` are allowed

#### Scenario: A prefix covers the file it names

- **GIVEN** a policy listing `.aws/credentials` as a prefix
- **WHEN** an agent's process opens `.aws/credentials`, `.aws/credentials.bak`, a file in `.aws/credentials.d/`, or `.aws/credentials.pub`
- **THEN** the first two are decided against and the others are not

#### Scenario: Off watches nothing

- **GIVEN** a pushed policy in off mode
- **WHEN** the guard sets up its muting and an agent's process opens an SSH key
- **THEN** nothing is muted, and the open is allowed and not reported

#### Scenario: A policy the extension cannot read is turned away

- **GIVEN** a pushed policy with an unknown mode, no mode, no credentials, or that is not a policy
- **WHEN** the extension receives it
- **THEN** it is turned away and the policy in force is unchanged

#### Scenario: A credential the extension cannot guard is skipped

- **GIVEN** a pushed policy with an unknown kind, an unknown match, an absolute path, a path with a `..`, empty or control character segment, or a prefix ending in `/`
- **WHEN** the extension receives it
- **THEN** those credentials are skipped and counted, the rest are enforced, and an owner naming neither a team nor a signing identifier is dropped

#### Scenario: An older policy does not replace a newer one

- **GIVEN** a policy accepted at version 1
- **WHEN** the version 0 policy, or version 1 again, arrives later
- **THEN** it is turned away
- **AND** a restarted extension enforces version 1

### Requirement: Credential block event

In protect mode the extension SHALL refuse an open it decides against and report it as `agent_credential_block`, with the same payload as `agent_credential_would_block`. A refusal the kernel did not take SHALL be reported as `agent_credential_would_block`.

#### Scenario: A refused read is reported

- **GIVEN** the guard in protect mode
- **WHEN** an agent's `cat` opens an SSH key
- **THEN** the open is refused and an `agent_credential_block` event names the process, the path and the credential kind
- **AND** had the kernel not taken the refusal, the event would be `agent_credential_would_block`
