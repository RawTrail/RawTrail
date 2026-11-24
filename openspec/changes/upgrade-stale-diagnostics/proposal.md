## Why

An in-place upgrade on one Mac lost network telemetry until a reboot, while a clean Mac with a user logged in cut over without one (#1542). The reboot cleared the state before anyone recorded what launchd and the system extension database held, so the cause cannot be found from what was kept. The one observation that was recorded, the network extension's Mach service missing from `launchctl print system` while both extensions read activated enabled, was taken by hand.

## What changes

- When the receiver logs the reboot-required hint, once per stale episode, the line now carries the product's lines from `systemextensionsctl list` (each extension version and its state) and from `launchctl print system` (the extensions' launchd jobs and Mach services). The next occurrence then records whether `group.io.rawtrail.networkextension` was registered and which versions were in which state.
- Gathering is best-effort and bounded by the probe's timeout. A tool that fails contributes a line saying so.

## Impact

- `agent/receiver/upgradeprobe.go`, `agent/receiver/loop.go`, `agent/cmd/rawtrail-agent/{main,sensors_notwindows}.go`. No behavior changes beyond the log line; #1542 stays open until a recurrence shows the cause.
