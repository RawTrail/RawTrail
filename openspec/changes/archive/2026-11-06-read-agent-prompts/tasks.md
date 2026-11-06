## 1. Permission

- [x] 1.1 Register `agent_prompt.read`, granted to admin and senior_analyst, outside the sampled read set

## 2. Read

- [x] 2.1 Read a session's prompts from the event archive, oldest first, paged and searchable
- [x] 2.2 Serve them at `GET .../agent-sessions/{session_id}/prompts` behind both gates

## 3. Tests

- [x] 3.1 Role matrix and audit-set cases, archive integration, and the handler's gates, pages and failures
