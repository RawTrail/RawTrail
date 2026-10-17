## ADDED Requirements

### Requirement: Copied agent tool calls

A tool call whose `origin_session_id` or `forked_from_session_id` names a session on the same host that holds a call with the same `tool_use_id` and owner SHALL be treated as a copy of that call. The session's tool calls SHALL NOT list a copy.

#### Scenario: A copied call is listed under its origin only

- **GIVEN** a fork holding a copy of its parent's call, and a session whose own call names a session that never held it, as after `/clear`
- **WHEN** each session's tool calls are read
- **THEN** the copy is listed under the parent only, and the other call under its own session, as is another user's line naming the parent

### Requirement: Agent session lineage endpoint

`GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage` SHALL return `forked_from_session_id`, the parent the session's fork job names, or else the origin of its latest copied call, and `forked_session_ids`, the sessions whose parent it is, sorted, counting only calls owned by the uid of the session's earliest process and linking only sessions that uid owns. It SHALL be gated on process read for the host and answer 404 for a session the host does not hold.

#### Scenario: A session names its parent and its forks

- **GIVEN** a parent, its fork, and a fork of the fork that copied calls from both
- **WHEN** each session's lineage is read
- **THEN** the parent names the fork only, the fork names the parent and the fork of the fork, and the fork of the fork names the fork

#### Scenario: Another user's transcripts do not make a lineage

- **GIVEN** another user's two transcripts, one named for this session and holding a copy of a call in the other
- **WHEN** this session's lineage is read, and that user's own session's
- **THEN** neither names the other, since a link stands only between sessions one user owns, and a session with no retained process names none

#### Scenario: A fork's job names its immediate parent

- **GIVEN** a fork of a session cleared with `/clear`, and a fork of a fork that made no calls, each carrying the parent its job names
- **WHEN** their lineage is read
- **THEN** each names that parent, and the cleared session's call it copied is listed under the cleared session only

#### Scenario: A fork without its own evidence is read as the earlier session's

- **GIVEN** a fork with no job, as before Claude Code 2.1.301: a fork of a fork that made no calls, and a fork made after `/clear` whose copies name the process's first session
- **WHEN** their lineage is read
- **THEN** the first reads as the earlier session's fork, and the second names no parent and lists its copies of the cleared session's calls as its own
