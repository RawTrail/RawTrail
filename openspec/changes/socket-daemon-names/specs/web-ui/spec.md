## ADDED Requirements

### Requirement: Socket daemon names

The Activity view SHALL name the daemon beside a `connect` row whose socket path matches a well-known daemon's: Docker and its Mac runtimes, Podman, ssh-agent, 1Password's SSH agent, PostgreSQL, MySQL, Redis, and mDNSResponder. It SHALL name none for another path or another operation.

#### Scenario: A connect to a well-known socket names its daemon

- **GIVEN** a session's connect rows to launchd's ssh-agent socket and to an unknown socket, and a delete of `/var/run/docker.sock`
- **WHEN** the operator opens the Files list
- **THEN** the ssh-agent row names "ssh-agent", and neither the unknown socket nor the delete names a daemon
