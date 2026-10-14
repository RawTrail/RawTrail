## ADDED Requirements

### Requirement: Process search by lineage

`GET /api/search/processes` SHALL accept `agent`, `agent_session`, `story_root`, `ancestor` and `args`, composing with the other filters. `ancestor` SHALL keep the process's descendants by creator and re-exec edges, not the process itself. `args` SHALL match a substring of one argument, case-sensitively. A `story_root` or `ancestor` that is not a positive id SHALL be rejected with `400 invalid_process`.

#### Scenario: A search keeps a session, a story or a process's descendants

- **GIVEN** a story holding a Claude Code tool call's shell, its curl and the curl's re-exec, beside other sessions' curls and one no agent ran
- **WHEN** the search filters by `agent=claude-code` and a path of `curl`, by the session, by the story's root, or by `ancestor` of the shell
- **THEN** it returns the curls of every Claude Code session, the session's processes, the story's processes, or the curl and its re-exec
- **AND** a `story_root` of `0` is rejected with `invalid_process`

#### Scenario: Arguments are matched one at a time

- **GIVEN** a process whose arguments are `curl`, `-s` and `https://example.com`
- **WHEN** the search filters by `args`
- **THEN** `example.com` matches it, while `EXAMPLE.COM` and `curl -s`, which spans two arguments, do not
