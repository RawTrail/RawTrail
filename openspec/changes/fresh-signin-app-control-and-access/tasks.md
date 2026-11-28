## 1. Application control

- [x] 1.1 Add the seven application control write actions to `requires_fresh_auth`.
- [x] 1.2 Policy tests: each write is refused on a stale session and allowed on a fresh one, and reading is not gated (Rego suite, the engine's per-action table, and the property test's mirror of the set).
- [x] 1.3 A Playwright journey against the local dex: a stale session's new rule prompts for identity, comes back after the round trip with the notice, and is stored only on the second save.
- [x] 1.4 Update the operator docs that list the gated actions.
