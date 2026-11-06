## Why

The session view can say what an agent ran but not what it was asked (#1426). The prompts a person typed are in the session transcript the agent already reads for tool calls, which the spec forbade it to send. The v0.9.0 epic (#1421) decided prompts are captured by default, redacted, with a switch to keep them on the Mac.

## What changes

- The agent reads each prompt a person typed to Claude Code, or passed to it with `claude -p`, from its transcript and sends it as an `agent_prompt` event. A slash command reads as `/name args`. The agent's own turns, its notifications, a compacted history, tool results and turns from an agent too old to record their origin are not prompts.
- Before a prompt leaves the Mac, recognizable credentials are replaced with `[REDACTED:<kind>]` and the text is cut to 16 KiB. The event carries the length as given, the cut, and the count of credentials replaced.
- Each `agent_tool_call` names the turn it was made in (`prompt_id`), which its prompt also names. A call before any turn the transcript names carries none.
- `EDR_CAPTURE_PROMPTS=0` turns prompt capture off on a Mac; tool calls are still sent.
- Model output and tool results are still never sent.

## Impact

- `agent/agenttranscript`: the prompt parser, the redaction, `prompt_id` on tool calls, the reader's switch.
- `agent/config`: `EDR_CAPTURE_PROMPTS`.
- `schema/events.json`: `agent_prompt` and its payload; `prompt_id` on `agent_tool_call`.
- `server/metrics`: `agent_prompt` among the counted event types.
- Not changed: the server's reads. Showing and searching prompts, the permission to read them and the admin switch are follow-ups (#1426, #1322, #1428).
