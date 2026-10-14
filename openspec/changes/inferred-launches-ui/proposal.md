## Why

The server links what launchd started to the `open` or `launchctl` that asked for it (`inferred-launch-links`, #1286), but the UI still shows the launched process where its parentage puts it: a root under launchd, unrelated to the agent's tool call. On edr-dev, TextEdit opened by a Claude Code session showed as a separate root in the session view rather than under the `open -a TextEdit` call.

## What changes

- A launched process, with what it started, can be hung under its launcher, and the rows moved are remembered so they are drawn as inferred rather than as lineage.
- The session view always does so: a launch is listed under the call's `open` or `launchctl`, with an "inferred launch" badge explaining it is linked by timing and target, and it is never folded with identical siblings.
- The host graph offers a "Logical tree" toggle, shown only when the read carries links. The strict OS tree stays the default. The logical tree draws an inferred launch's edge dashed and labels the node "inferred launch".

## Impact

- `ui/src/types.ts`, `ui/src/components/launchLinks.ts`, `ui/src/components/{ProcessTree,ProcessTree.render}.ts(x)`, `ui/src/components/Sessions/{SessionView,sessionTree}`, and their tests.
