# agent-configuration Specification

## Purpose

Defines the agent's intentionally minimal configuration surface: the small set of supported settings, their safe defaults fixed as constants, and the rule that an unrecognized or removed setting is ignored rather than failing the agent's start.

## Requirements

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

### Requirement: A proxy the agent cannot speak is refused rather than used

The agent SHALL determine whether it can speak a configured proxy's scheme before connecting to it, and SHALL NOT open a connection to a proxy whose scheme it cannot speak. Credentials configured on a proxy address are sent to whatever host that address names, so a proxy the agent was never going to be able to use MUST NOT receive them: an operator's secret crossing the network in the clear is a cost paid before the agent discovers the setting was unusable, and no retry of an unusable setting may pay it again.

The decision SHALL be made where the agent resolves its proxy, so that every consumer reaches the same answer. The agent's HTTP traffic, its control channel, and the lifeline address a contained host is pinned to are all derived from one proxy setting, and a component deciding this for itself would leave a contained host pinned to a proxy the rest of the agent does not dial.

Resolving the agent's proxy SHALL NOT yield a proxy whose scheme it cannot speak, whatever code path assembled the configuration. A check applied only while reading the configuration file would be a convention that the next caller to construct that configuration directly can bypass unknowingly.

Where a configured proxy is refused, the agent SHALL connect directly and SHALL continue to run. An endpoint whose agent will not start is an unmonitored endpoint, which is a worse outcome than one that reports and keeps working, and the traffic concerned reaches only the agent's own configured server.

The agent SHALL report the refusal at startup, naming the setting and its scheme. Without that, the operator sees only connection failures and no indication that the setting they wrote is the cause.

It SHALL report that it is connecting directly only where that is true. The settings are refused independently, so a host may have one the agent cannot speak and another carrying all of its traffic; stating a direct connection there would describe the opposite of what the agent is doing.

#### Scenario: A refused proxy receives nothing

- **GIVEN** a proxy configured with a scheme the agent does not support, carrying credentials in its address
- **WHEN** the agent connects to its server, by any of its paths
- **THEN** no connection is made to the host that address names and no bytes are sent to it
- **AND** the credentials are not transmitted

#### Scenario: The operator is told which setting was refused

- **GIVEN** a proxy setting whose scheme the agent cannot speak
- **WHEN** the agent starts
- **THEN** it reports the setting's name and its scheme
- **AND** it reports connecting directly, because no usable proxy remains
- **AND** the agent starts and keeps running

#### Scenario: A refused setting does not imply a direct connection

- **GIVEN** one proxy setting the agent cannot speak and another it can, both configured
- **WHEN** the agent starts
- **THEN** it reports the refused setting by name and scheme
- **AND** it does NOT report connecting directly, because the traffic goes through the proxy it can speak

#### Scenario: A supported proxy is unaffected

- **GIVEN** a proxy configured with a scheme the agent supports, with or without credentials
- **WHEN** the agent connects to its server
- **THEN** the proxy is used for every path as before, including the lifeline address a contained host is pinned to
