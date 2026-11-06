## ADDED Requirements

### Requirement: Agent session prompts

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/prompts` SHALL list the prompts a person typed in the session, oldest first, each with its text as the agent sent it (redacted, at most 16 KiB), its turn, its length as typed and its time. It SHALL page them by `cursor`, keep those whose text holds `q` regardless of case, refuse a malformed cursor with 400 `invalid_cursor`, and require both process read for the host and `agent_prompt.read`.

#### Scenario: A session's prompts page in the order they were typed

- **GIVEN** a session with more prompts than a page holds
- **WHEN** a client follows `next_cursor` to the end
- **THEN** it reads every prompt of that session once, oldest first, and none of another session's

#### Scenario: A search keeps the prompts whose text holds it

- **WHEN** a client asks with `q`
- **THEN** only the session's prompts whose text holds it, regardless of case, are listed

#### Scenario: Prompt read is required on top of process read

- **GIVEN** an operator allowed process read for the host but not `agent_prompt.read`
- **WHEN** they ask for the session's prompts
- **THEN** the request is refused with 403
