## Why

The prompt search on the Agent sessions page listed each prompt with an absolute timestamp and no session (#1560). The rest of the console shows recent times as an age, with the exact time on hover. When several prompts matched, nothing on a row said whether they came from one session or several, short of opening each link.

## What changes

- **Typed** reads as an age (`formatRelativeNs`), with the exact time as the cell's title. It is still the link to the prompt's turn on the session's Timeline.
- A **Session** column names the prompt's session by the first 8 characters of its id, the way the sessions list does. The full id is the title, and the cell links to the session's own view.
- The id-shortening rule moves to `sessionPath.ts`, so the sessions list and the prompt search share one rule.

## Impact

- `ui/src/components/Sessions/PromptSearch.tsx`, `SessionList.tsx`, `sessionPath.ts`. UI only: the search response already carries each prompt's session id.
