## Why

The server holds a fleet-wide prompt capture switch (#1426), on by default, and existing fleets start capturing prompts when they upgrade. Administrators need to see that plainly and turn it off without calling the API.

## What changes

- A Data collection section in Admin settings, under `detection_config.read`, with the prompt capture switch, a plain statement of what is captured, and who last changed it.
- Switching asks for a reason, sends the version the page loaded, and reports how far the push reached; a conflicting change offers the latest settings.
- The CHANGELOG's action-required note names the switch.

## Impact

- `ui/src/components/AgentSettings/` (new), the Settings sections and routes, `ui/src/api.ts`, and a shared `PushNotice` moved out of `WatchedPaths`.
