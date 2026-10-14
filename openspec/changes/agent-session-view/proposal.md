## Why

The Sessions page (#1283) lists what each AI agent session touched in aggregate, but answering "what did this session do" still meant reading a host's whole process graph around the session's time. An agent's activity is a sequence of tool calls, each a shell running a command the model wrote, and the graph shows neither the sequence nor the command: every call is a `zsh -c` whose script hides the command inside the agent's own plumbing.

## What changes

- A session view at `/sessions/<host>/<session>`, opened from the Sessions page. It reads the session's processes by its index (`scope=session`) and lists them in the order the tool calls ran, one group per call.
- Each group is labelled with the command the agent ran, read from the `eval '<command>'` argument of Claude Code's wrapper script, with single-quote escapes decoded. A root that is not such a wrapper is labelled with its own command line.
- Under each group, everything the call started, including a daemon it backgrounded or double-forked, since the server builds the forest by creator.
- The wrapper shell and the fork-only shell copies a subshell makes are hidden unless "Show shell plumbing" is on.
- Each process opens in its host's graph, selected at its fork time.

Folding identical children, capping and "show more", search, alert and privilege badges, and in-page process detail follow in a second change.

## Impact

- `ui/src/{api,types,App}.ts(x)`, `ui/src/components/Sessions/{SessionView,sessionTree,toolCommand}`, `ui/src/test/agentSession.ts`, and their tests.
- `test/fakeagent/scenarios/claude-code-session.yaml` and `test/e2e/tests/qa/agent-sessions.spec.ts`.
