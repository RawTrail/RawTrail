## ADDED Requirements

### Requirement: The reboot hint carries the host's extension state

When the agent logs that an upgrade left the network extension's registration stale, the log line SHALL carry what the host showed at that moment: each of the product's extension versions and its state, and the launchd jobs and Mach services naming them. It SHALL be gathered once per stale episode, with the hint.

#### Scenario: The reboot hint carries what the host showed

- **GIVEN** a network extension the agent cannot reach after an upgrade, with a previous version pending removal and the new one enabled
- **WHEN** the agent logs the reboot-required hint
- **THEN** the line carries each extension version with its state and the launchd lines naming the extensions, including whether `group.io.rawtrail.networkextension` is registered
- **AND** it is gathered once for the episode, not on every failed attempt
