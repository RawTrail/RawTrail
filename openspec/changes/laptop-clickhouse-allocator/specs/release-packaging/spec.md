## MODIFIED Requirements

### Requirement: The laptop ClickHouse is sized for one Mac's inserts

The laptop profile's ClickHouse config SHALL size its background merge pool for one Mac's inserts (at most 4 threads), SHALL log at warning level or quieter, and SHALL keep no text log or query log. Its allocator SHALL keep one arena. The installer SHALL recreate ClickHouse when that config changes.

#### Scenario: The config keeps ClickHouse's background work small

- **GIVEN** the ClickHouse config the laptop compose profile mounts
- **WHEN** its pool, logging and system log settings are read
- **THEN** the background merge pool has at most 4 threads, the log level is warning or quieter, and the text log and the query log are removed

#### Scenario: ClickHouse's allocator keeps one arena

- **GIVEN** the laptop compose profile's ClickHouse service
- **WHEN** its environment is read
- **THEN** it sets `MALLOC_CONF` to disable the allocator's arena for each core and keep one arena, since an arena a core held twice the memory ClickHouse had allocated and ClickHouse counts that against its cap

#### Scenario: A changed config recreates ClickHouse

- **GIVEN** a laptop install whose ClickHouse config file has changed since its container was created, as an upgrade's checkout brings
- **WHEN** the installer starts the stack
- **THEN** it labels the ClickHouse service with the config's digest, so compose recreates the container with the new config
