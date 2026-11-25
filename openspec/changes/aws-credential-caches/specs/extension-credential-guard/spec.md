## MODIFIED Requirements

### Requirement: Credential files are guarded in every home

The extension SHALL decide each open of these files in root's home and every person's: `~/.ssh/id_*` except names ending in `.pub`, `~/.aws/credentials`, every file directly in `~/.aws/sso/cache/` and `~/.aws/cli/cache/`, `~/.config/gh/hosts.yml`, `~/.netrc`, `~/.docker/config.json` and `~/.kube/config`. It SHALL receive opens of those files only, through target-path muting inverted to them, and SHALL NOT let the kernel cache a verdict, since the verdict depends on the opener.

#### Scenario: Every home's credential files are guarded

- **GIVEN** a host with root's home and a person's home
- **WHEN** the guard sets up its muting
- **THEN** each of the eight credentials is muted in both homes, root's in both its `/var` and `/private/var` spellings
- **AND** the SSH keys are muted as the prefix `~/.ssh/id_`, the two AWS caches as the prefixes `~/.aws/sso/cache/` and `~/.aws/cli/cache/`, and the other five as literal files

#### Scenario: Public keys and SSH config are not credentials

- **GIVEN** an agent's process
- **WHEN** it opens `~/.ssh/id_ed25519.pub`, `~/.ssh/id_rsa-cert.pub`, `~/.ssh/config` or `~/.ssh/known_hosts`
- **THEN** the open is allowed and nothing is reported

#### Scenario: The AWS credential caches are guarded

- **GIVEN** the built-in policy in protect mode
- **WHEN** an agent's `cat` opens a file in `~/.aws/sso/cache/` or `~/.aws/cli/cache/`, and the AWS CLI, signed by its team, opens the same files
- **THEN** the agent's opens are refused and reported as AWS credentials, and the AWS CLI's are allowed

## ADDED Requirements

### Requirement: A directory credential covers the files directly in it

A credential matched as a directory SHALL cover every file directly in that directory, whatever its name, and SHALL NOT cover a file in a subdirectory, a sibling sharing the directory's name, or the directory itself. The extension SHALL skip a directory credential at the top of the home or written with a trailing `/`.

#### Scenario: A directory covers the files directly in it

- **GIVEN** a policy listing `.aws/sso/cache` as a directory
- **WHEN** an agent's process opens a file in `.aws/sso/cache/`, a file in a subdirectory of it, `.aws/sso/cache.bak`, or the directory itself
- **THEN** the first is decided against and the others are not
- **AND** a pushed directory of `.aws` or `.aws/sso/cache/` is skipped and counted
