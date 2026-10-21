## Why

An agent that starts a launchd job with `launchctl kickstart` or `launchctl start`, or opens an app by bundle id with `open -b`, was not linked to what launchd started: the inferred-launch correlation (#1286) matched only an executable path, and these commands name a job label or a bundle id. Measured on edr-dev (#1341): launchd's xpcproxy row for a job names its label, and TextEdit's code-signing identifier is the bundle id `open -b` named. A throttled restart came 5.0 s after `kickstart -k`.

## What changes

- **`launchctl kickstart [flags] <domain>/<label>` and `launchctl start <label>`:** linked to the program launchd re-exec'd from the xpcproxy row naming that label.
- **`open -b <bundle id>`:** linked to the launchd child whose code-signing identifier is the bundle id, ignoring case.
- **Window:** label and bundle-id matches are exact, so they may start up to 15 s after the command, enough for launchd's restart throttle. Path matches keep 5 s.
- **Not covered:** `launchctl load` and `bootstrap` name a plist, not the label inside it; a follow-up has the agent read it.

## Impact

- `server/detection/internal/graph/launch.go`, its unit tests, an integration test, the OpenAPI description and the CHANGELOG.
