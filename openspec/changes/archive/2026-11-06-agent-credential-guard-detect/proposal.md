## Why

The main attack on a coding agent is a prompt injection that makes it read `~/.ssh/id_ed25519` or `~/.aws/credentials` and send the contents out. The agent's own deny rules are advisory and any shell command bypasses them. Nothing in RawTrail observed these reads: application control decides only execs and always allows Apple binaries such as `cat`, watched paths ignore reads, and no rule covers them (#1431, epic #1421).

## What changes

- A fifth Endpoint Security client, the credential guard, decides every open of a credential file in every home: SSH private keys, AWS CLI credentials, the GitHub CLI's `hosts.yml`, `.netrc`, Docker's `config.json` and the Kubernetes config. Target-path muting is inverted to those files, so no other open on the host reaches it.
- An open is decided against when the opener is an AI agent's process and not one of the credential's owner tools, matched by signing identity: `ssh` and its helpers for SSH keys, the vendor's signed CLI for the others. `git push` runs `ssh` and stays quiet; `cat ~/.ssh/id_ed25519` from an agent does not.
- A fork by the Claude Code binary now starts an agent subtree, for the guard and for agent file activity alike. A session that started before the extension was invisible to both until its commands carried a session id, and Claude's own helpers (`rg` for its Grep tool, `git`) carry none.
- Detect mode only, as the default: every open is allowed, and a decided-against open is reported as a new `agent_credential_would_block` event. Protect mode, the server rule, the policy switch and the session badges are follow-ups (#1432, #1433).

## Impact

- `extension/edr/extension/CredentialGuard.swift`, `CredentialGuardSubscriber.swift`, `AgentFileSubscriber.swift` (the process set's lifecycle handling, now shared), `main.swift`.
- `schema/events.json`, the OpenAPI event type enum and the ingest metrics' type set gain `agent_credential_would_block`. The server stores the events; no rule reads them yet.
