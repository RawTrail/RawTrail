## ADDED Requirements

### Requirement: Credential files are guarded in every home

The extension SHALL decide each open of these files in root's home and every person's: `~/.ssh/id_*` except names ending in `.pub`, `~/.aws/credentials`, `~/.config/gh/hosts.yml`, `~/.netrc`, `~/.docker/config.json` and `~/.kube/config`. It SHALL receive opens of those files only, through target-path muting inverted to them, and SHALL NOT let the kernel cache a verdict, since the verdict depends on the opener.

#### Scenario: Every home's credential files are guarded

- **GIVEN** a host with root's home and a person's home
- **WHEN** the guard sets up its muting
- **THEN** each of the six credentials is muted in both homes, root's in both its `/var` and `/private/var` spellings
- **AND** the SSH keys are muted as the prefix `~/.ssh/id_`, the other five as literal files

#### Scenario: Public keys and SSH config are not credentials

- **GIVEN** an agent's process
- **WHEN** it opens `~/.ssh/id_ed25519.pub`, `~/.ssh/id_rsa-cert.pub`, `~/.ssh/config` or `~/.ssh/known_hosts`
- **THEN** the open is allowed and nothing is reported

### Requirement: Agents read credentials only through their tools

An open SHALL be decided against when the opener is an AI agent's process and not one of the credential's owner tools. An agent's process is one in an agent subtree, or the Claude Code binary itself. Owners SHALL be matched by signing identity: an Apple platform binary's signing identifier, or a Developer ID team. In detect mode, the default and the only mode this change ships, such an open SHALL be allowed and reported as an `agent_credential_would_block` event.

#### Scenario: An agent reading an SSH key is reported

- **GIVEN** a Claude Code session in detect mode
- **WHEN** it runs `cat ~/.ssh/id_ed25519`
- **THEN** the open is allowed
- **AND** an `agent_credential_would_block` event names `cat`'s `pid` and `pidversion`, the key's path and `credential` `ssh_private_key`

#### Scenario: Claude Code is an agent before its exec is seen

- **GIVEN** a Claude Code process that started before the extension did, so no agent subtree holds it
- **WHEN** its own Read tool opens `~/.aws/credentials`
- **THEN** an `agent_credential_would_block` event is reported for `aws_credentials`

#### Scenario: A push over SSH is not reported

- **GIVEN** a Claude Code session
- **WHEN** it runs `git push` to an SSH remote, and `git` runs `ssh`, which opens the key
- **THEN** no event is reported, because `ssh` is a platform binary with signing identifier `com.apple.ssh`

#### Scenario: A terminal read is not reported

- **GIVEN** a terminal that no agent started
- **WHEN** the person runs `cat ~/.ssh/id_ed25519`
- **THEN** the open is allowed and no event is reported

#### Scenario: An ad-hoc owner identifier is not an owner

- **GIVEN** an agent's process that is not a platform binary but whose signing identifier is `com.apple.ssh`
- **WHEN** it opens an SSH key
- **THEN** an `agent_credential_would_block` event is reported

### Requirement: The guard fails open

The extension SHALL answer each open from in-memory state, before it serializes any event, and SHALL allow every open it does not decide against. It SHALL NOT refuse an open by a process outside an agent session in any mode. A client that cannot set up its muting SHALL stay unsubscribed rather than receive every open on the host.

#### Scenario: A process outside an agent session is never refused

- **GIVEN** the guard in protect mode
- **WHEN** a process outside any agent session opens a credential it does not own
- **THEN** the open is allowed and nothing is reported

### Requirement: Credential would-block event

The `agent_credential_would_block` payload SHALL carry `pid`, `pidversion`, the opened `path`, the `credential` kind (`ssh_private_key`, `aws_credentials`, `github_cli_token`, `netrc`, `docker_config` or `kube_config`) and the open's access mode as `flags`, spelled as open(2) spells `O_RDONLY`, `O_WRONLY` and `O_RDWR`.

#### Scenario: The payload round-trips and validates

- **GIVEN** a would-block payload for any credential kind, process generation and access mode
- **WHEN** the extension encodes it
- **THEN** it decodes to the same payload and carries exactly those five keys
- **AND** `schema/events.json` accepts it, and refuses one without `pidversion` or with an unknown `credential`
