## ADDED Requirements

### Requirement: Own binaries are self-allowed

Before consulting the active snapshot, the extension SHALL ALLOW any AUTH_EXEC whose target carries the release team ID and a code-signing identifier RawTrail signs one of its own binaries with: the agent, either extension, or the host app. The agent SHALL be matched by the identifier it is signed with. A binary that carries the release team ID and any other identifier SHALL be decided by the snapshot.

#### Scenario: The agent starts under a block on the release team

- **GIVEN** an active snapshot with a TEAMID BLOCK rule in PROTECT for the release team ID
- **WHEN** the agent, signed with the release team ID and its signing identifier, is executed
- **THEN** the extension allows the exec
- **AND** the same exec would be blocked by the snapshot without the failsafe

#### Scenario: Another binary from the release team is not self-allowed

- **GIVEN** a binary signed with the release team ID and an identifier outside RawTrail's own set, such as the agent's launchd label
- **WHEN** it is executed
- **THEN** the failsafe does not apply and the snapshot decides the exec

#### Scenario: The self-allow entry is the identifier the agent is signed with

- **GIVEN** the identifier the release build and the dev build sign the agent with
- **WHEN** the self-allow set is consulted for it under the release team ID
- **THEN** the agent is self-allowed
