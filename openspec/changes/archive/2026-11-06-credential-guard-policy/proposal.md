## Why

The credential guard (#1431) runs in detect mode over a list built into the extension. An operator cannot switch it to protect or off, add a second SSH key or an in-house signed tool as an owner, or stop protecting a file. Issue #1432, the policy's server half.

## What changes

- `GET` and `PUT /api/v1/detection-config/credential-guard`: the guard's mode (detect, protect, off) and the credentials it protects, each a kind, a path below every home (a file, or a name prefix in a directory), and the tools that own it by signing identity. Version 0 is detect mode over the built-in credentials, each marked default.
- A change is validated, needs a reason and `detection_config.write` (which takes a fresh sign-in), is audited with the previous and new policy, and is pushed to every enrolled host as `set_credential_guard`; hosts that miss it are caught up, the unchanged default included.
- The agent and the extension apply it in a following change; until then hosts receive the command and do not act on it.

## Impact

- `server/rules` (migration `00012`, `api`, `internal/credentialguard`, the detection-config handler, bootstrap), `server/identity/api` (the audit action), `docs/api/openapi.yaml`.
