## ADDED Requirements

### Requirement: The laptop ClickHouse is sized for one Mac's inserts

The laptop profile's ClickHouse config SHALL size its background merge pool for one Mac's inserts (at most 4 threads), SHALL log at warning level or quieter, and SHALL keep no text log or query log, so that a steady stream of the agent's small inserts does not grow ClickHouse to its memory cap. The installer SHALL recreate ClickHouse when that config changes.

#### Scenario: The config keeps ClickHouse's background work small

- **GIVEN** the ClickHouse config the laptop compose profile mounts
- **WHEN** its pool, logging and system log settings are read
- **THEN** the background merge pool has at most 4 threads, the log level is warning or quieter, and the text log and the query log are removed

#### Scenario: A changed config recreates ClickHouse

- **GIVEN** a laptop install whose ClickHouse config file has changed since its container was created, as an upgrade's checkout brings
- **WHEN** the installer starts the stack
- **THEN** it labels the ClickHouse service with the config's digest, so compose recreates the container with the new config
