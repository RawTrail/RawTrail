## Why

A laptop install is one person watching their own AI agents, and the console was built for a security team: it opens on the alert queue and puts single sign-on, containment and service accounts in front of someone who has no use for them (issue #1436).

## What changes

- `EDR_PROFILE` (`server`, the default, or `laptop`) names the deployment's profile, and `GET /api/session` reports it. The laptop compose profile sets it.
- On a laptop the console opens on Agent sessions, and Admin settings offers single sign-on, containment and service accounts only once **Show advanced settings** is on. The choice is kept in the browser. Every page stays reachable; nothing is granted or removed.
- The credential guard already starts in detect mode, so the laptop needs no change there.
- A later change in this issue adds the first-run checklist.

## Impact

- Server: config, the session probe, the laptop compose file.
- UI: the profile context, the home redirect, the settings navigation and the account menu's settings entry.
- Docs: install-server's configuration table, OpenAPI, changelog.
