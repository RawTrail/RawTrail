## Why

The credential guard's policy can be changed only through `PUT /api/v1/detection-config/credential-guard` (issue #1432). An operator who wants to switch it to protect, add a credential or allow their own signed tool has no place in the console to do it.

## What changes

- The Detection tuning page gains an **AI agent credential guard** section, beside Watched file paths: the mode, each protected credential in plain words with the tools allowed to read it and the built-ins marked, and who saved it last.
- An operator with `detection_config.write` edits a draft of the whole policy and saves it with a reason, as for the watched paths: a fresh sign-in when needed, the server's refusal shown as written, a conflicting change offered as the latest policy, and unsaved work kept across a reauthentication.
- `docs/operations.md` explains the guard and its modes.

## Impact

- UI: `ui/src/components/DetectionConfig/CredentialGuard.tsx`, `credentialOwners.ts`, `DetectionConfig.tsx`, `api.ts`.
- No server change.
