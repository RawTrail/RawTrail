## ADDED Requirements

### Requirement: Process search by lineage

`GET /api/search/processes` SHALL accept `agent`, `agent_session` (with `host_id`), `story_root`, `ancestor` and `args`, composing with the other filters. `ancestor` SHALL keep each descendant once, by creator and re-exec edges, up to 900 levels down, not the process itself. `args` SHALL match a substring of one argument, case-sensitively. A non-positive `story_root` or `ancestor` SHALL be rejected with `400 invalid_process`.

#### Scenario: A search keeps a session, a story or a process's descendants

- **GIVEN** a story holding a Claude Code tool call's shell, its curl and the curl's re-exec, beside other sessions' curls and one no agent ran
- **WHEN** the search filters by `agent=claude-code` and a path of `curl`, by the session, by the story's root, or by `ancestor` of the shell
- **THEN** it returns the curls of every Claude Code session, the session's processes, the story's processes, or the curl and its re-exec
- **AND** a `story_root` of `0` is rejected with `invalid_process`

#### Scenario: Arguments are matched one at a time

- **GIVEN** a process whose arguments are `curl`, `-s` and `https://example.com`
- **WHEN** the search filters by `args`
- **THEN** `example.com` matches it, while `EXAMPLE.COM` and `curl -s`, which spans two arguments, do not

#### Scenario: A process's descendants are each listed once

- **GIVEN** a shell that re-execs three times, each generation forking a child, and an ancestor whose story was not seen above a child whose story was
- **WHEN** the search filters by `ancestor` of either
- **THEN** each generation and child is listed once, and the child with a story is listed under the ancestor without one

#### Scenario: A deep chain is walked to its bound

- **GIVEN** a fork chain 1,100 processes deep
- **WHEN** the search filters by `ancestor` of its top
- **THEN** it counts the 900 descendants within the bound rather than failing
