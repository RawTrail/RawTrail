## Why

The security extension's credential guard reports an AI agent's process reading a credential file that is not its tool's (#1431), but no rule raised an alert from it, so the reports reached the archive and nobody was told. Issue #1432, first part.

## What changes

- A detection rule, `agent_credential_read` (T1552.004, T1552.001), alerts on each `agent_credential_would_block` report, naming the credential in plain words ("tried to read your AWS credentials").
- One alert per session and kind of credential: a session's later reads of the same kind join the first. A process with no session (the agent's own) is alerted on its own.
- A kind of credential the server does not name yet, from a newer agent, is still alerted, as "a credential file".
- An efficacy corpus scenario, `T1552.004-agent-credential-read`, and regression fixtures.

## Impact

- `server/rules/internal/catalog`, the generated `docs/detection-rules.md` and rule pack, `test/efficacy/corpus/`, and the fake agent's event types.
