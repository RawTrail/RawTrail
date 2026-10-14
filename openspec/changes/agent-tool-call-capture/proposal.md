## Why

The session view groups an AI agent's processes by tool call, labelled with the command the shell ran, but nothing ties a group to the tool call that caused it: its `tool_use_id`, the description the agent gave it, or which of two identical commands it was (#1288).

## What changes

- The agent reads the session transcripts Claude Code keeps at `~/.claude/projects/<project>/<session id>.jsonl` under every home directory, every 5 seconds, tailing each recently changed transcript from where it left off.
- Each tool call becomes an `agent_tool_call` event stamped at the transcript's time: the session, the `tool_use_id`, the tool, a Bash call's command (at most 8 KiB) and description, a file tool's path, the working directory, and whether a subagent made it.
- Only tool-call metadata leaves the host. Prompt text, model output and tool results are never read into an event.
- A transcript line the agent cannot read is skipped and counted, never guessed at.

The transcript was chosen over a hook (which needs the user's Claude Code settings edited) and the OTel event (which needs telemetry enabled and a collector), because it needs no setup.

## Impact

- `agent/agenttranscript`, `agent/sensorevent` (an exported envelope marshal), the agent's sensor startup.
- `schema/events.json`, the OpenAPI enum, and the ingest metrics' type set gain `agent_tool_call`. The server stores the events; reading them into the session view is the next change.
