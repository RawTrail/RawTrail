## Why

An agent captures no prompts until it has the fleet's agent settings (`set_agent_settings`, issue #1426). A host running an agent older than the command fails it as an unknown command type, and the catch-up counts a failed command as delivered for six hours, so after that host is upgraded in place the new agent waits up to six hours for its settings and sends no prompts meanwhile (issue #1465). The same holds for the credential guard policy and the watched-path set, which older agents fail the same way.

## What changes

- A host's check-in records, on the database clock, when it reports an agent version other than the stored one (`enrollments.agent_changed_at`, migration `00011`).
- The catch-up of every pushed document (watched paths, agent settings, credential guard policy) treats a command queued before that time as not on the host, as it already does for one queued before a re-enrollment.

## Impact

- Endpoint context: the migration, the check-in's inventory update, and the active enrollments.
- Rules context: `HostEnrollment.AgentChangedAt` and `hostpush.Needs`.
