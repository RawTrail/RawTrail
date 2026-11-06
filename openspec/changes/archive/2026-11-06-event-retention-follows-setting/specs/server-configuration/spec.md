## ADDED Requirements

### Requirement: Event retention follows a setting

The server SHALL keep raw events in the event archive for `EDR_EVENT_RETENTION_DAYS` days, default 30, accepting 1 to 36500 and refusing to start on any other value. At startup the server SHALL set the archive's TTL to the setting when it holds another value, and SHALL leave the table unaltered when it already matches.

#### Scenario: The setting governs which events expire

- **GIVEN** `EDR_EVENT_RETENTION_DAYS=7`
- **WHEN** the server starts and the archive applies its TTL
- **THEN** an event ingested 10 days ago is expired and one ingested 3 days ago is kept

#### Scenario: An unchanged setting does not alter the table

- **GIVEN** the archive's TTL already matches `EDR_EVENT_RETENTION_DAYS`
- **WHEN** the server starts
- **THEN** it issues no ALTER on the events table

#### Scenario: An out-of-range setting stops startup

- **GIVEN** `EDR_EVENT_RETENTION_DAYS` is 0, negative, or above 36500
- **WHEN** the server loads its configuration
- **THEN** loading fails naming the setting
