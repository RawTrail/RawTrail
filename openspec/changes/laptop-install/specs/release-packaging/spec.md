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

`rawtrail-laptop install` SHALL refuse without a running Docker engine, SHALL keep its secrets, certificate and server image across reruns and later commands, SHALL write a 0600 agent config with the localhost URL, enroll secret and certificate SHA-256 before installing the package, SHALL install only a notarized package, and SHALL open the admin setup link the running server printed.

#### Scenario: No Docker engine is running

- **GIVEN** a Mac whose Docker engine is stopped
- **WHEN** the install runs
- **THEN** it exits non-zero saying no engine is running, and starts nothing

#### Scenario: The agent config pins the server certificate

- **GIVEN** a fresh install
- **WHEN** it finishes
- **THEN** the agent config names `https://localhost:8443`, the generated enroll secret, and the SHA-256 of the certificate the server serves, and the setup link the server printed is opened

#### Scenario: An existing config is rewritten private

- **GIVEN** an agent config readable by every local user
- **WHEN** the install writes it
- **THEN** the config is mode 0600

#### Scenario: A rerun keeps the secrets

- **GIVEN** a completed install
- **WHEN** the install runs again
- **THEN** every secret and the certificate are unchanged

#### Scenario: A declined certificate trust is asked again

- **GIVEN** a self-signed certificate whose keychain trust was declined
- **WHEN** the install runs again
- **THEN** it asks for the trust again for the same certificate

#### Scenario: A package that is not notarized is not installed

- **GIVEN** an agent package Gatekeeper rejects
- **WHEN** the install runs with it
- **THEN** it exits non-zero and the package is not installed

#### Scenario: A server whose admin is set up opens the sign-in page

- **GIVEN** a server that printed no setup link since it last started
- **WHEN** the install or `login` looks for one
- **THEN** it opens the sign-in page instead of an earlier start's link

#### Scenario: Login keeps the installed server image

- **GIVEN** an install of a chosen release
- **WHEN** `login` runs
- **THEN** it restarts the server without pulling or recreating it from another image

### Requirement: The laptop install leaves another server's agent alone

The install SHALL read the agent config by the agent's own rules, SHALL refuse to repoint an agent configured for another server unless asked to replace its config, SHALL leave the agent as it was when the package fails to download, verify or install, SHALL restore a replaced config on uninstall, and uninstall SHALL leave installed an agent that reports elsewhere.

#### Scenario: An agent reporting elsewhere is not repointed unasked

- **GIVEN** a Mac whose agent config names another server
- **WHEN** the install runs without `--replace-config`
- **THEN** it exits non-zero naming that server, and the config is unchanged

#### Scenario: A failed package install leaves the agent as it was

- **GIVEN** an agent config naming another server, and a package that is rejected or fails to install
- **WHEN** the install runs with `--replace-config`
- **THEN** it exits non-zero, and the agent's config and its enrollment are unchanged

#### Scenario: A replaced config is restored on uninstall

- **GIVEN** an install run with `--replace-config` over another server's config
- **WHEN** the laptop install is uninstalled
- **THEN** the agent's enrollment with the other server was dropped at install, and its config is back after uninstall

#### Scenario: The agent config is read as the agent reads it

- **GIVEN** an agent config naming the laptop server with spaces around `=`, a lowercase key, or quotes, or naming it before a later entry for another server
- **WHEN** the laptop install is uninstalled
- **THEN** the agent is removed in the first three cases and left installed in the last

#### Scenario: Uninstall leaves another server's agent installed

- **GIVEN** a Mac whose agent reports to another server
- **WHEN** the laptop install is uninstalled
- **THEN** the agent's uninstaller does not run and its config is unchanged

### Requirement: Uninstall removes what the laptop install added

The install SHALL NOT adopt an existing non-empty directory it did not make. `rawtrail-laptop uninstall` SHALL run the package's uninstaller for an agent reporting to the laptop server, SHALL stop with the config and stack kept while that uninstaller is left for a retry, and SHALL then remove the config, the stack's containers and volumes, the keychain trust it added and its state directory.

#### Scenario: Uninstall removes the agent, the stack and the state

- **GIVEN** a completed install whose certificate was trusted in the login keychain
- **WHEN** it is uninstalled
- **THEN** the agent's uninstaller runs, the config is gone, the stack is brought down with its volumes, the trust is removed, and the state directory is gone

#### Scenario: Uninstall waits for the agent's extensions to go

- **GIVEN** an agent whose extensions the package's uninstaller could not remove, leaving itself for a retry
- **WHEN** the laptop install is uninstalled
- **THEN** it exits non-zero, and the agent's config and the stack are kept

#### Scenario: An existing directory is never adopted

- **GIVEN** a state directory that already holds files and was not made by the install
- **WHEN** the install runs
- **THEN** it exits non-zero and the directory is unchanged
