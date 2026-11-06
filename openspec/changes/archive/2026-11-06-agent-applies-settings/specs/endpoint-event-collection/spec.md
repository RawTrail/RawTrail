## ADDED Requirements

### Requirement: Prompt capture follows the fleet agent settings

The agent SHALL send the prompts people type to AI agents only while both its own `EDR_CAPTURE_PROMPTS` and the fleet's `capture_prompts` setting allow it. A change to the setting SHALL take effect for the next line read, without a restart. The agent SHALL NOT capture before it has the fleet's settings, from a push or from its record of the last applied, which it SHALL keep across restarts.

#### Scenario: Turning capture off takes effect without a restart

- **GIVEN** an agent capturing prompts
- **WHEN** it applies settings with `capture_prompts` false
- **THEN** a prompt typed afterwards is not sent, while tool calls still are

#### Scenario: An older push does not undo a newer one

- **GIVEN** an agent that applied settings with `capture_prompts` false
- **WHEN** an earlier `set_agent_settings` with `capture_prompts` true arrives late
- **THEN** capture stays off

#### Scenario: A new agent waits for the fleet settings

- **GIVEN** an agent with no record of the fleet's settings
- **WHEN** it starts
- **THEN** it does not capture prompts until the server sends the settings, and then follows them

#### Scenario: A restart keeps the settings last applied

- **GIVEN** an agent that applied settings with `capture_prompts` false
- **WHEN** it restarts
- **THEN** it does not capture prompts before the server sends anything
- **AND** an agent whose record of the settings is damaged does not capture either
