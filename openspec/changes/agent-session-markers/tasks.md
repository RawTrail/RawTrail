## 1. Extension

- [x] 1.1 Add the agent session marker allowlist and a full-name filter with a value cap.
- [x] 1.2 Read `es_exec_env` on exec and add `env` to the exec payload, omitted when empty.

## 2. Schema and tests

- [x] 2.1 Document `env` in `schema/events.json`, closed to the allowlisted names.
- [x] 2.2 Swift tests pin the allowlist match, the messaging-token negative case, the value cap and the omission.
- [x] 2.3 The server exec payload models `env`, and a property test pins that any marker map round-trips and an absent one stays absent.

## 3. QA

- [x] 3.1 On edr-dev, a command run by a real Claude Code session carries `env.CLAUDE_CODE_SESSION_ID` equal to its transcript filename, and no event carries `CLAUDE_CODE_MESSAGING_TOKEN`.
