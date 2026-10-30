## Why

The server can search the prompts people typed across agent sessions (#1487), but nothing in the console asked it. Issue #1428, UI half.

## What changes

- A Search prompts panel on the Agent sessions page, for an operator who may read prompts: text, a window of the last day, week or 30 days, and the results newest first with their time, host and a snippet around the match, a page at a time.
- Each result links to its session opened on its Conversation, where the prompt reads with what the agent did for it. A session's view opens on the view a link names in `?view=`.
- The panel says that every search is recorded in the audit log with its text.

## Impact

- `ui/src/components/Sessions/` (the panel, its snippet, the session list and view), `ui/src/api.ts`, `ui/src/types.ts`, `ui/src/permissions-core.ts`, the conversation journey.
