## Why

The agent sends the prompts a person types to an AI agent (#1450), but nothing on the server reads them back, so the session view cannot say what the agent was asked. A prompt can hold anything the person typed, so reading one needs its own permission and a complete audit trail. Issue #1426, server half.

## What changes

- New action `agent_prompt.read`, granted to `admin` and `senior_analyst` (and `super_admin` by its wildcard), not to `analyst` or `auditor`. Process read does not imply it. It is not a sampled read, so every read is audited.
- New `GET /api/hosts/{host_id}/agent-sessions/{session_id}/prompts`: the session's prompts oldest first, paged by cursor, searchable by `q`, gated on process read and on `agent_prompt.read`.

## Impact

- `server/identity` (action registry, policy data), `server/visibility` (the archive read), `server/detection/internal/operator` (the route), `docs/api/openapi.yaml`, `ui/src/permissions-core.ts`.
- Follow-ups in #1426: a server-pushed capture switch, and a dedicated prompts table when the fleet-wide search (#1428) needs one.
