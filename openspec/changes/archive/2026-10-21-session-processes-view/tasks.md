## 1. UI

- [x] 1.1 A Processes lens with one unfolded tree rooted at the session's agent, and its process count.
- [x] 1.2 A reparented badge from the server's `reparented` flag, and an agent badge on the agent's row.
- [x] 1.3 Rows named by executable; the agent opens from the Activity tables.

## 2. Tests and QA

- [x] 2.1 vitest for the forest, the row search and both views.
- [x] 2.2 The fake agent sends `original_ppid`; the scenario adds the agent's exec and an adopted sleep; the journey covers Processes.
- [x] 2.3 Chrome on the lane A dev server, on the e2e session and a real edr-dev session.
