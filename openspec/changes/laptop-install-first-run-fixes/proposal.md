## Why

Running the laptop install on a real Mac against v0.9.0-rc.8 (#1435) found three problems a first-time user would hit:

- The installer refused the notarized agent package as "not notarized": it ran Gatekeeper's check as the user on a copy only root can read.
- After registering the admin passkey, nothing led to the setup checklist; it is reachable only from the account menu.
- On a Mac whose upgrade left the previous network extension registered until a restart (#1542), the checklist said to click Allow on a prompt that never comes.

## What changes

- The installer runs the notarization check as root, on the same root-owned copy it installs.
- On a laptop install, the console opens the setup checklist right after the first admin's passkey is registered. Later visits to the root still open Agent sessions.
- The checklist's network filtering step asks for a restart, as a step that needs the person, when the agent reports the network extension needs one.

## Impact

- `scripts/laptop/rawtrail-laptop`, `ui/src/App.tsx`, `ui/src/auth.ts`, `ui/src/components/BreakGlassSetup.tsx`, `ui/src/components/Setup/setupSteps.ts`, `docs/watch-your-agent.md`.
