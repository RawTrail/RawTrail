## Why

The server holds a fleet-wide prompt capture switch and pushes it as `set_agent_settings` (#1426), but the agent only reads `EDR_CAPTURE_PROMPTS` at startup, so turning the switch off would change nothing on the Macs.

## What changes

- The agent applies `set_agent_settings` when it is later than the settings in force, ordered by epoch and then version, and acknowledges an older one without applying it.
- The transcript reader asks whether to capture for each line, so a change takes effect without a restart.
- The settings last applied are kept in `agent-settings.json` beside the event queue. A restart honours them at once; a file that cannot be read turns capture off until the next push.
- `EDR_CAPTURE_PROMPTS=0` stays a local hard off that the server cannot undo.

## Impact

- New `agent/agentsettings` package; `agent/commander` and `agent/controlclient` gain the command; `agent/agenttranscript` takes a capture function rather than a startup flag.
- Must be exercised on a live macOS VM before RC.
