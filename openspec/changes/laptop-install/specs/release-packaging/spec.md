## ADDED Requirements

### Requirement: The laptop stack is reachable from its own Mac only

The laptop compose profile SHALL publish the server on 127.0.0.1 only and no other service on any host port, SHALL have the server terminate TLS itself with the installer's certificate for localhost, and SHALL set no telemetry export endpoint.

#### Scenario: Only the server is published, on the loopback interface

- **GIVEN** the laptop compose profile
- **WHEN** its services' published ports are read
- **THEN** the server's one port is bound to 127.0.0.1 and no other service publishes a port

#### Scenario: The laptop server exports no telemetry

- **GIVEN** the laptop compose profile
- **WHEN** the server's environment is read
- **THEN** it sets no `OTEL_EXPORTER_OTLP_ENDPOINT` and names a TLS certificate and key of its own

### Requirement: The laptop stack is sized for a laptop

The laptop profile SHALL cap ClickHouse's memory near 1 GB with ClickHouse's own setting, under a container limit above it, and SHALL keep events and process graphs for 7 days alike.

#### Scenario: ClickHouse refuses a query before the kernel kills it

- **GIVEN** the laptop compose profile and the ClickHouse config it mounts
- **WHEN** the memory settings are read
- **THEN** ClickHouse's own cap is at most 1.1 GB and below the container's memory limit

#### Scenario: Events and process graphs expire together after a week

- **GIVEN** the laptop compose profile
- **WHEN** the server's retention settings are read
- **THEN** both `EDR_EVENT_RETENTION_DAYS` and `EDR_RETENTION_DAYS` are 7

### Requirement: One command installs the laptop stack and the agent

`rawtrail-laptop install` SHALL refuse without a running Docker engine, SHALL keep the secrets and certificate it made across reruns, SHALL write the agent config with the localhost URL, the enroll secret and the certificate's SHA-256 fingerprint before installing the package, SHALL install only a notarized package, and SHALL open the admin setup link.

#### Scenario: No Docker engine is running

- **GIVEN** a Mac whose Docker engine is stopped
- **WHEN** the install runs
- **THEN** it exits non-zero saying no engine is running, and starts nothing

#### Scenario: The agent config pins the server certificate

- **GIVEN** a fresh install
- **WHEN** it finishes
- **THEN** the agent config names `https://localhost:8443`, the generated enroll secret, and the SHA-256 of the certificate the server serves, and the setup link the server printed is opened

#### Scenario: A rerun keeps the secrets

- **GIVEN** a completed install
- **WHEN** the install runs again
- **THEN** every secret and the certificate are unchanged

#### Scenario: A package that is not notarized is not installed

- **GIVEN** an agent package Gatekeeper rejects
- **WHEN** the install runs with it
- **THEN** it exits non-zero and the package is not installed

### Requirement: The laptop install leaves another server's agent alone

The install SHALL refuse to repoint an agent configured for another server unless asked to replace its config, SHALL then keep that config and restore it on uninstall, and uninstall SHALL leave installed an agent that reports to another server.

#### Scenario: An agent reporting elsewhere is not repointed unasked

- **GIVEN** a Mac whose agent config names another server
- **WHEN** the install runs without `--replace-config`
- **THEN** it exits non-zero naming that server, and the config is unchanged

#### Scenario: A replaced config is restored on uninstall

- **GIVEN** an install run with `--replace-config` over another server's config
- **WHEN** the laptop install is uninstalled
- **THEN** the agent's enrollment with the other server was dropped at install, and its config is back after uninstall

#### Scenario: Uninstall leaves another server's agent installed

- **GIVEN** a Mac whose agent reports to another server
- **WHEN** the laptop install is uninstalled
- **THEN** the agent's uninstaller does not run and its config is unchanged

### Requirement: Uninstall removes what the laptop install added

`rawtrail-laptop uninstall` SHALL run the agent package's uninstaller for an agent reporting to the laptop server and remove its config, remove the stack's containers and volumes, remove the certificate's keychain trust the install added, and remove the install's state directory.

#### Scenario: Uninstall removes the agent, the stack and the state

- **GIVEN** a completed install whose certificate was trusted in the login keychain
- **WHEN** it is uninstalled
- **THEN** the agent's uninstaller runs, the config is gone, the stack is brought down with its volumes, the trust is removed, and the state directory is gone
