## 1. Rule

- [x] 1.1 `agent_credential_read` over `agent_credential_would_block`, in plain words, one alert per session and kind
- [x] 1.2 Registered, with its platforms, docs and rule pack file

## 2. Tests

- [x] 2.1 Fixtures: an SSH key read by cat, AWS credentials by python, and a kind the server does not name
- [x] 2.2 Unit: the subject per session and kind, the agent's own process, every kind's words, exclusions, what is not a report
- [x] 2.3 The efficacy scenario through the in-process pipeline
