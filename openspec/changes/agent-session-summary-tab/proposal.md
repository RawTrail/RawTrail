## Why

An agent session's views list what it did row by row. Nothing says, at a glance, what the session was asked, what it ran, what it touched and where it talked to. Issue #1429, UI half, on the digest the summary endpoint now carries.

## What changes

- A **Summary** view, listed first among the session's views, built only on the session summary:
  - at a glance: the agent, its project, how long it ran, its prompts and its commands;
  - what it used: tool calls by tool, the programs it ran most;
  - what it touched: files by operation, file events by directory relative to the project, and the paths outside the project;
  - where it talked to: lookups and connections, domains grouped as model APIs, Git hosts, package registries and everything else, a domain new to the host marked;
  - effort: tokens and the time in each permission mode, bypass flagged;
  - a timeline of commands and of events over the session's span;
  - alerts as a count with a link to Commands, last.
- Each section says plainly when the session did none of it.
- Commands stays the view a session opens on.

## Impact

- `ui/src/components/Sessions/` (the Summary, its shaping, the view switcher).
- The cost estimate waits on #1427; sharing the Summary as an image is #1430.
