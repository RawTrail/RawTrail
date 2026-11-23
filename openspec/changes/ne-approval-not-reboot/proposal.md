## Why

After an in-place upgrade, the setup checklist told a person to restart the Mac when the network extension only needed approving (#1598). The agent reported `reboot_required` whenever a previous network extension version was waiting to uninstall on reboot. That old version stays registered until the next reboot whatever else is going on, so it was also there while the new version waited for approval, and approving it is what connected the agent.

## What changes

- The agent reports `reboot_required` for the network extension only when a previous version is waiting to uninstall on reboot AND the new version is activated and enabled, which is the stranded registration a restart finishes (#1542).
- A new version waiting for approval, or switched off in System Settings, keeps the generic connect failure and `never_connected`, so the checklist asks for the approval rather than a restart.

## Impact

- `agent/receiver/upgradeprobe.go`.
- No server, UI or wire change: the checklist already shows the approval step for `never_connected`.
