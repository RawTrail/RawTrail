## ADDED Requirements

### Requirement: Data collection settings show and change prompt capture

Admin settings SHALL include a Data collection section, under `detection_config.read`, that says plainly whether agents capture the prompts people type to AI coding agents and what that means, and who last changed it. An operator with `detection_config.write` SHALL be able to switch capture on or off; the change SHALL require a reason, SHALL name the version it started from, and SHALL report how far its push reached.

#### Scenario: The page says plainly that prompts are captured

- **GIVEN** agent settings with prompt capture on
- **WHEN** an operator opens Data collection settings
- **THEN** the switch is on and the page says agents send the prompts people type to AI coding agents
- **AND** it says that `EDR_CAPTURE_PROMPTS=0` on a Mac keeps them there whatever the setting

#### Scenario: Turning capture off asks for a reason and reports the push

- **GIVEN** an operator permitted `detection_config.write`
- **WHEN** they switch capture off and give a reason
- **THEN** the change is sent with the reason and the version the page loaded
- **AND** the page shows the new state and how many enrolled hosts the change was queued for
- **AND** when someone else changed the settings first, the page says so and offers to load the latest
