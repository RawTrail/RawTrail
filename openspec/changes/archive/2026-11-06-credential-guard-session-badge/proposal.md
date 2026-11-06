## Why

When an AI agent's command reads a credential file, the session view shows only that the command has an alert. The operator has to open the alert to learn what happened, when the point of the credential guard (issue #1432) is to say it plainly where the command is.

## What changes

- A command in the session view that read a credential, or was stopped from reading one, says so in words beside it, open or closed: "Tried to read your AWS credentials" or "Stopped from reading your SSH private key", each linking to its alert.

## Impact

- UI: `ui/src/components/Sessions/credentialRead.ts`, `SessionView.tsx`. No server change.
