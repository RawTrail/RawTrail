# extension-credential-guard Specification

## Purpose

The credential guard is the system extension's check on opens of well-known credential files (SSH private keys, cloud, GitHub, Docker and Kubernetes credentials) in every home on the Mac. It lets an AI agent session read them only through the tools meant to use them, reports every other read, and in protect mode refuses it, so a prompt injection or a poisoned dependency cannot quietly take the developer's keys. It fails open: any open it does not decide against is allowed, and an open outside an agent session is never refused.

## Requirements

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

### Requirement: The guard enforces the pushed policy

The extension SHALL enforce the newest credential guard policy pushed (by epoch, then version), persisted across restarts, and the built-in one until a push arrives. Its mode and credentials replace the built-ins: protect refuses what detect reports, off watches nothing. An open SHALL be decided by the most specific credential covering it, once that file is muted. An unknown mode SHALL turn the policy away; an unusable credential SHALL be skipped.

#### Scenario: A pushed policy replaces the built-in one

- **GIVEN** a pushed policy in protect mode over AWS credentials, a `.netrc` in a subdirectory and a second SSH key prefix with no owners
- **WHEN** an agent's process opens each
- **THEN** each open is refused and reported, and an SSH key the policy no longer lists is not guarded
- **AND** exactly those three credentials are muted in each home

#### Scenario: The most specific credential decides

- **GIVEN** a protect policy listing `.netrc` owned by curl and `work/.netrc` owned by no tool, and the SSH key prefix owned by OpenSSH and `.ssh/id_deploy` owned by no tool, in either order
- **WHEN** an agent's curl opens `work/.netrc` and its ssh opens `.ssh/id_deploy`
- **THEN** both opens are refused, while curl's `.netrc` and ssh's `.ssh/id_rsa` are allowed

#### Scenario: A prefix covers the file it names

- **GIVEN** a policy listing `.aws/credentials` as a prefix
- **WHEN** an agent's process opens `.aws/credentials`, `.aws/credentials.bak`, a file in `.aws/credentials.d/`, or `.aws/credentials.pub`
- **THEN** the first two are decided against and the others are not

#### Scenario: Off watches nothing

- **GIVEN** a pushed policy in off mode
- **WHEN** the guard sets up its muting and an agent's process opens an SSH key
- **THEN** nothing is muted, and the open is allowed and not reported

#### Scenario: A policy the extension cannot read is turned away

- **GIVEN** a pushed policy with an unknown mode, no mode, no credentials, or that is not a policy
- **WHEN** the extension receives it
- **THEN** it is turned away and the policy in force is unchanged

#### Scenario: A credential the extension cannot guard is skipped

- **GIVEN** a pushed policy with an unknown kind, an unknown match, an absolute path, a path with a `..`, empty or control character segment, a prefix ending in `/`, or a prefix at the top of the home
- **WHEN** the extension receives it
- **THEN** those credentials are skipped and counted, the rest are enforced, and an owner naming neither a team nor a signing identifier is dropped

#### Scenario: An older policy does not replace a newer one

- **GIVEN** a policy accepted at version 1
- **WHEN** the version 0 policy, or version 1 again, arrives later
- **THEN** it is turned away
- **AND** a restarted extension enforces version 1

### Requirement: Credential block event

In protect mode the extension SHALL refuse an open it decides against and report it as `agent_credential_block`, with the same payload as `agent_credential_would_block`. A refusal the kernel did not take SHALL be reported as `agent_credential_would_block`.

#### Scenario: A refused read is reported

- **GIVEN** the guard in protect mode
- **WHEN** an agent's `cat` opens an SSH key
- **THEN** the open is refused and an `agent_credential_block` event names the process, the path and the credential kind
- **AND** had the kernel not taken the refusal, the event would be `agent_credential_would_block`
