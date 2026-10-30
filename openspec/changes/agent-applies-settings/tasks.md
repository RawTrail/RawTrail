## 1. Agent

- [x] 1.1 `agentsettings.Settings`: open from the state directory, apply a newer push, persist through a renamed temporary file.
- [x] 1.2 `set_agent_settings` in the executor, wired into both transports.
- [x] 1.3 The transcript reader asks the settings per line.
- [x] 1.4 Tests: ordering, restart, damaged and unreadable files, local off, write failure, both transports, and a mid-run switch.

## 2. QA

- [x] 2.1 Live macOS VM (edr-dev, 2026-10-29): with no settings the agent sent a tool call and not the prompt; the catch-up delivered the settings 52 s after enrollment and the next prompt was sent; turning capture off reached the agent in about a second and the next prompt was not sent; after a restart it still sent none.
