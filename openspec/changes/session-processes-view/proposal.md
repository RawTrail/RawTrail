## Why

The session view's Commands lens groups a session's processes for reading: one group per command, shell plumbing hidden, identical siblings folded, and the agent process itself left out. An investigator also needs the forensic lens, every process the session ran as one tree, which mature EDRs keep beside the narrative one (Elastic's Session View next to its Analyzer, SentinelOne's OS tree next to its Storyline) (#1335).

## What changes

- A third session lens, **Processes**, between Commands and Activity: one tree rooted at the agent process the session's tree read names (#1334), with every process beneath it, nothing hidden or folded, and a count of what it lists.
- A process launchd adopted after its creator exited (a double fork, `nohup`) stays under its creator and is badged as reparented, in both Commands and Processes.
- The agent's row is badged as the agent, and every row is named by its executable, so Claude Code's versioned image reads as `claude`.
- A process the Activity tables name opens its detail even when it is the agent, which is not among the session's roots.
- The fake agent, in Go and in the e2e fixture, sends an exec's `original_ppid`, so scenarios can model an adopted process.

## Impact

- `ui/src/components/Sessions/`, `ui/src/types.ts`, `test/fakeagent/`, `test/e2e/fixtures/agent.ts`, the agent-sessions scenario and journey, and the CHANGELOG.
