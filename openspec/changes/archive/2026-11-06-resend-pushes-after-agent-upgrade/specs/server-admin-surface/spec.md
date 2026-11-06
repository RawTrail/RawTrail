## ADDED Requirements

### Requirement: Pushed documents are resent after an agent upgrade

The catch-up of each pushed document (watched paths, agent settings, credential guard policy) SHALL treat a command queued before the host's agent was last replaced in place as not on the host, as it treats one queued before the host's latest enrollment, so an upgraded agent receives the document within one catch-up interval.

#### Scenario: An upgraded agent is sent the settings

- **GIVEN** a host whose older agent failed the agent settings as an unknown command ten minutes ago
- **WHEN** the catch-up runs before the agent is upgraded, and again after a check-in reports the new version
- **THEN** the first run sends nothing, since a failure waits six hours, and the second sends the settings
