## MODIFIED Requirements

### Requirement: The agent configuration surface is intentionally minimal

The agent SHALL treat its upload batch size, upload interval, queue prune age, network/DNS coalescing window and SQLite queue byte cap as fixed constants, and SHALL limit operator configuration to enrollment, connection, identity, diagnostic and outbound-proxy settings, read from `/etc/rawtrail.conf` with environment-variable overrides. The proxy SHALL apply to every connection the agent makes to the server, or to none. An environment variable the agent no longer recognizes SHALL be inert.

#### Scenario: A removed tuning variable is ignored at startup

- **GIVEN** a host whose conf file or environment sets a no-longer-recognized variable (for example `EDR_BATCH_SIZE` or `EDR_NETWORK_COALESCE_WINDOW`)
- **WHEN** the agent loads its configuration
- **THEN** it starts successfully using the fixed default
- **AND** the variable has no effect on behavior

#### Scenario: The queue byte cap remains enforced though not configurable

- **GIVEN** the agent's SQLite queue reaches its fixed byte cap
- **WHEN** new events are enqueued
- **THEN** over-cap rows are dropped and counted in the queue-dropped metric
- **AND** no configuration is required or accepted to change the cap

#### Scenario: A proxy set in the conf file is used

- **GIVEN** a host whose `/etc/rawtrail.conf` sets a proxy (`HTTP_PROXY`, `HTTPS_PROXY` or `NO_PROXY`, in either case) and whose process environment sets none
- **WHEN** the agent loads its configuration and connects to the server
- **THEN** its enrollment, uploads, command polling, lifeline target and control channel all go through that proxy
- **AND** a proxy set in the process environment instead overrides the file, as every other setting does

### Requirement: Default file locations are platform-specific

The agent SHALL resolve its default configuration-file, event-queue, and enrollment-token locations from the platform it runs on: `%ProgramData%\RawTrail\` on Windows, and `/etc/rawtrail.conf` plus `/var/db/rawtrail/` on macOS and Linux. An operator MAY still override any location through the existing environment variables; the platform default applies only when the override is unset.

#### Scenario: The agent resolves platform-appropriate default paths

- **GIVEN** an agent started with no path-override environment variables
- **WHEN** it resolves its default configuration
- **THEN** the configuration-file, event-queue, and enrollment-token paths are the platform's default locations
