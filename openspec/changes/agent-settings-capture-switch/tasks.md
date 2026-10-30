## 1. Server

- [x] 1.1 `agent_settings` table and `agentsettings` store, service and converger on the shared `hostpush` push and catch-up.
- [x] 1.2 `GET` and `PUT /api/v1/detection-config/agent-settings`, audited as `detection_config.agent_settings_update`.
- [x] 1.3 Integration tests for the read, the change, its push and audit row, the refusals, and the catch-up.

## 2. Follow-up

- [ ] 2.1 The agent applies `set_agent_settings` and the settings page offers the switch.
