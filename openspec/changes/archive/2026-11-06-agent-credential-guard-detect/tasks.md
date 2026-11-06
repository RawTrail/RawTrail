## 1. Extension

- [x] 1.1 `CredentialGuard`: the protected credentials, their owner tools and the decision over path, process, owner and mode, with Swift tests.
- [x] 1.2 `CredentialGuardSubscriber`: AUTH_OPEN muted to the credential files in every home, its own agent process set from fork, exec and exit, answered uncached before anything is serialized.

## 2. Contract

- [x] 2.1 `agent_credential_would_block` in `schema/events.json`, the OpenAPI enum and the ingest metrics' type set, with a schema test and a Swift wire round-trip.

## 3. QA

- [ ] 3.1 edr-dev: a Claude Code session's `cat ~/.ssh/id_ed25519` is reported; its `git push` over SSH is not; a terminal `cat` outside the agent is not. AUTH_OPEN answer latency measured.
