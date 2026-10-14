## Why

An agent session's tree read returns the processes stamped with the session, each command's wrapper shell as a root. The agent process carries no session marker, so it is not in the tree, and a view of every process the session ran cannot be rooted at the agent that ran them (#1334, for the Processes view in #1335).

## What changes

- A session's tree read also returns the session's agent processes, in an `agents` field, found as the session's activity read finds them: the creator of a root whose pid is the session's agent pid.
- `roots`, the counts and truncation are unchanged, so the session view's command groups are unaffected.
- The OpenAPI spec documents `agents`, and `parent_process_id` on a process, which links each root to its agent.

## Impact

- `server/detection/internal/operator/handler.go`, `server/detection/api/types.go`, `docs/api/openapi.yaml`, and a handler test.
