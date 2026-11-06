## Why

Prompt capture is on by default (#1426), and the only way to turn it off today is `EDR_CAPTURE_PROMPTS=0` in each agent's configuration. An administrator whose privacy or works-council obligations rule out prompt capture needs one fleet-wide switch.

## What changes

- The server holds the agent settings: one versioned document of fleet-wide switches for what agents collect, starting with `capture_prompts`, on by default.
- `GET` and `PUT /api/v1/detection-config/agent-settings` read and change them, with a reason and an optional expected version, under the detection-config permissions.
- A change is audited in the same transaction, and queues a `set_agent_settings` command for every enrolled host. Hosts that miss it are caught up by the same sweep as the watched-path set.
- The watched-path push, its held audit entry and its catch-up move into a shared `hostpush` package that both documents use. Watched paths behave as before.

## Impact

- `server/rules`: new `agentsettings` package, migration `00011_agent_settings.sql`, routes, and the shared `hostpush` package.
- `server/identity/api`: audit action `detection_config.agent_settings_update`.
- The agent applying the command, and the settings page, follow in the next change.
