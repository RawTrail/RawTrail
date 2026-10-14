## 1. UI

- [x] 1.1 Read a session's forest with `getSessionTree`.
- [x] 1.2 Parse the agent's command out of the tool-call wrapper (`toolCommand`).
- [x] 1.3 Group the forest by tool call and hide shell plumbing unless verbose (`sessionTree`).
- [x] 1.4 Add the session view, its route, and the Sessions page link to it.

## 2. Tests

- [x] 2.1 vitest: the parser on the commands an edr-dev session ran and on quoting edge cases; grouping and plumbing on that session's forest; the page.
- [x] 2.2 Playwright: from the Sessions page into a session, one group per tool call with its command and a backgrounded daemon inside its call.
