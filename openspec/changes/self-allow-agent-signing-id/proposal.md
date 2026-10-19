## Why

The Endpoint Security extension allows its own components to execute before it consults the application-control snapshot, so a misconfigured rule cannot block the EDR itself. It recognized the agent by `io.rawtrail.agent`, which is the agent's launchd label, while the agent is code-signed with the identifier `rawtrail-agent`. The agent was therefore never self-allowed: a block on the release team ID, or on the agent's certificate, would stop the agent from starting again (#1373).

## What Changes

- The self-allow set names the agent by the identifier it is signed with, taken from the same constant the XPC peer check pins, so the two cannot disagree.
- The failsafe is specified: RawTrail's own binaries are allowed under any rule, and other binaries signed by the same team are not.

## Impact

- `extension/edr/extension/AuthExecDecider.swift` (the set and its predicate) and `ESFSubscriber.swift` (the caller).
