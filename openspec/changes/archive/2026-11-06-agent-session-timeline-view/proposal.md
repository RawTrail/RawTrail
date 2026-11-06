## Why

The session view's Conversation and Commands views listed the same command rows: Conversation grouped them under the prompt each was run for and added the tool calls that start no process, so it held everything Commands did. Two views of one list, each with its own search and paging, read as two different things.

## What changes

- One Timeline view replaces Conversation and Commands. A "Group by prompt" switch lists the commands under their prompts, with the tool calls that start no process, or alone in the order they started.
- It opens grouped when the session recorded a prompt and ungrouped when it recorded none. The operator's choice holds as they move between sessions.
- "Show shell plumbing" applies in both groupings; it applied to Commands alone before.
- A link naming the old views (`?view=conversation`, `?view=commands`) opens the Timeline grouped or ungrouped. A prompt search result links to `?view=timeline&group=prompt`, which opens it grouped whatever the operator last chose.
- The Summary's alerts link opens the Timeline ungrouped, where alerted commands open expanded.
- The prompt search box's placeholder reads "Words in a prompt".

## Impact

- `ui/src/components/Sessions/SessionView.tsx`, `SessionSummary.tsx`, `PromptSearch.tsx`, their tests, and the end-to-end journeys that open either view.
- The unarchived deltas `agent-session-conversation-view`, `agent-prompt-search-page` and `agent-session-view-pages-commands` are reworded to name the Timeline.
