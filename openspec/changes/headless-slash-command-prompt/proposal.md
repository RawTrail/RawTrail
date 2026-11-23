## Why

A slash command passed to `claude -p` was not captured as an `agent_prompt` (#1543). Claude Code runs it as a local command: it writes a meta `<local-command-caveat>` entry, then the command as plain text with neither an origin nor a prompt source. The prompt parser accepts a line only when its origin is `human`, or when it has no origin and the source `sdk`, so it read the command as a line the agent wrote and dropped it. The command's work still arrived (its usage and tool calls, under its turn), but the session showed no prompt for it.

## What changes

- A user entry with neither an origin nor a prompt source, whose content is plain text starting with `/`, is reported as the person's prompt when it follows Claude Code's local-command caveat entry (its `parentUuid` is the caveat's id). It reads as `/name args`, with its turn and the source `sdk`.
- Lines the agent writes stay excluded: they record an origin, or do not follow a caveat. An interactive local command such as `/clear`, which also follows a caveat with no origin, is recorded as its `<command-name>` tags, not as plain text starting with a slash, and is still not a prompt.

## Impact

- `agent/agenttranscript/parse.go`, `agent/agenttranscript/reader.go`. No wire, server or schema change.
