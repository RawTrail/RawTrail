## ADDED Requirements

### Requirement: Filter status subcommand

The host app SHALL provide a `filter-status` subcommand that prints `enabled` or `disabled` for the content filter's NetworkExtension configuration and exits successfully. It SHALL change no configuration and submit no extension request, and a preferences read that fails or does not answer SHALL exit with a failure.

#### Scenario: The filter status reads the configuration only

- **GIVEN** the host app
- **WHEN** `edr filter-status` runs
- **THEN** it prints `enabled` when the content filter's configuration is enabled and `disabled` when it is not
- **AND** it neither changes a configuration nor activates or deactivates an extension
