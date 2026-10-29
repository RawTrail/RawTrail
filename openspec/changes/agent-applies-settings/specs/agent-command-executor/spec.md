## ADDED Requirements

### Requirement: Set-agent-settings command

The system SHALL execute a `set_agent_settings` command carrying `{version, epoch, capture_prompts}` by putting the settings in force when the agent has none yet or they are later than those in force, ordered by `epoch` and then `version`, and SHALL report completed with the `version`, whether it was `applied`, and the `capture_prompts` in force afterwards. A payload with a negative `version`, or that cannot be recorded, SHALL be reported failed.

#### Scenario: The agent reports the settings in force

- **GIVEN** settings in force at epoch 9
- **WHEN** a `set_agent_settings` command arrives with a later epoch, and then one with an earlier epoch
- **THEN** the first is reported completed with `applied` true and the settings it carried
- **AND** the second is reported completed with `applied` false and the settings still in force
