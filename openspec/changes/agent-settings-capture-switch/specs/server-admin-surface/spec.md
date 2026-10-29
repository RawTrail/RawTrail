## ADDED Requirements

### Requirement: Agent settings are configured over the API

The server SHALL hold one versioned agent-settings document whose `capture_prompts` is true until changed. `GET /api/v1/detection-config/agent-settings` SHALL return it under `detection_config.read`. `PUT` SHALL replace it under `detection_config.write` only with an explicit `capture_prompts`, a non-blank reason and, when given, the current `expected_version`; it SHALL store the next version, audit it in the same transaction, and queue `set_agent_settings` for every enrolled host.

#### Scenario: Prompt capture is on until an administrator turns it off

- **GIVEN** settings no operator has changed
- **WHEN** a caller permitted `detection_config.read` reads them
- **THEN** the response carries version 0 and `capture_prompts` true

#### Scenario: Turning prompt capture off is stored, pushed and audited

- **GIVEN** a caller permitted `detection_config.write` and three enrolled hosts
- **WHEN** they set `capture_prompts` to false with a reason
- **THEN** the settings are stored as the next version with `capture_prompts` false
- **AND** a `set_agent_settings` command carrying `{version, epoch, capture_prompts}` is queued for each of the three hosts, the epoch being the settings' update time in Unix microseconds
- **AND** the change is audited as `detection_config.agent_settings_update` with the reason, the previous and new values, and the host counts

#### Scenario: A change without a value or a reason is refused

- **WHEN** a caller sends a change without `capture_prompts`, or with a blank reason
- **THEN** it is refused with 400 and nothing is stored or queued

#### Scenario: A change to settings that moved on is refused

- **GIVEN** settings changed since the caller read version 1
- **WHEN** the caller sends a change with `expected_version` 1
- **THEN** it is refused with 409 and nothing is stored or queued

### Requirement: Hosts that miss the agent-settings push get them

The server SHALL periodically queue the current agent settings for each enrolled host whose latest `set_agent_settings` command does not carry them, by the same rules as the watched-path catch-up. It SHALL queue nothing while the settings have never been changed.

#### Scenario: A host enrolled after a change is sent the settings

- **GIVEN** settings changed before a host enrolled
- **WHEN** the catch-up runs
- **THEN** a `set_agent_settings` command carrying the current settings is queued for that host, and for no host that already has them

#### Scenario: Unchanged settings are not sent

- **GIVEN** settings no operator has changed
- **WHEN** the catch-up runs
- **THEN** no command is queued
