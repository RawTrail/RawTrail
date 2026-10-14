## Why

The session view (`agent-session-view`, `agent-session-view-badges`) lists every process a session ran. A real session runs loops and long pipelines: a `for` loop over a directory lists one identical row per file, and a call that ran a thousand commands opens on all of them. An operator also needs to find one command in a long session, and to read a process's detail without leaving the session.

## What changes

- Identical processes under the same creator that started nothing, changed no privilege and carry no alert fold into one row, with the count and the span of their fork times. Folding is off when the read was truncated or the alerts could not all be read, since a row could then hide a child or an alert.
- A list longer than 20 rows shows its first 20 and a "Show N more processes" control. A row holding a search hit or an alert is listed past the cap.
- A search box finds a command or path, ignoring case. It expands the calls it finds something in, says how many processes match in each, and highlights them.
- A process's name selects it and opens the existing process detail beside the session, with a link to the process in its host's graph. The detail's "Show in timeline" opens the host's timeline on a window ending just after the process's last observed activity, so its own events are inside it.

## Impact

- `ui/src/components/Sessions/{SessionView,sessionTree}` and their tests; `ui/src/components/ProcessDetail.tsx` takes an optional timeline link.
- `test/e2e/tests/qa/agent-sessions.spec.ts`.
