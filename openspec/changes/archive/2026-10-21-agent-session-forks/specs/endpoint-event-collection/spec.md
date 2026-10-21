## ADDED Requirements

### Requirement: Agent tool call origin

When a transcript line records a tool call under a `session_id` other than its session's, the agent SHALL send that id as the call's `origin_session_id`. A line whose `session_id` is its session's, or absent, SHALL carry none.

#### Scenario: A copied call names the session it was written under

- **GIVEN** a forked session's transcript, whose copy of its parent's tool call keeps the parent's `session_id`
- **WHEN** the agent reads the line
- **THEN** it reports the call under the fork's session with `origin_session_id` naming the parent, and reports the fork's own calls with none

### Requirement: Fork parent from the job

When a session's Claude Code job (`~/.claude/jobs/<first eight characters of the session id>/parent-transcript.jsonl`, written by `/fork` in 2.1.301 and later) names another session, the agent SHALL send it as `forked_from_session_id` on that session's tool calls, read within the user's home as a regular file, bounded, and never for a subagent's transcript.

#### Scenario: A fork's calls name the session its job was forked from

- **GIVEN** a session whose job's parent transcript carries another session's id, and sessions with no job, a job naming the session itself, or a FIFO or a link out of the home in its place
- **WHEN** the agent reads their transcripts
- **THEN** only the first session's calls carry `forked_from_session_id`, naming that other session

