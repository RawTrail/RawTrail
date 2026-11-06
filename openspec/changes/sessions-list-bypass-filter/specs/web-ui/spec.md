## ADDED Requirements

### Requirement: Sessions list bypass badge

The Agent sessions page SHALL badge the row of each session that ran with permissions bypassed "Bypass permissions", the badge's title saying a fork of a bypassed session carries it too.

#### Scenario: A bypassed session's row is badged

- **GIVEN** two sessions on a host, one whose messages were written with permissions bypassed
- **WHEN** the operator opens the Agent sessions page
- **THEN** only the bypassed session's row is badged
