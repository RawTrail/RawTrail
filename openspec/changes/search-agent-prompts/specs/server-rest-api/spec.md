## ADDED Requirements

### Requirement: Agent prompt search

`GET /api/agent-prompts` SHALL find the prompts whose text holds `q` regardless of case, by a redelivered prompt's newest text, across every host's AI agent sessions or `host_id`'s, typed within the window, newest first, keyset-paged by a cursor that keeps the window, each with its host and session. It SHALL require `agent_prompt.read`, SHALL be authorized and audited as a search named by its query, and SHALL be served with `Cache-Control: no-store`.

#### Scenario: A search finds prompts across sessions newest first

- **GIVEN** prompts on two hosts in three sessions that hold "deploy" in any case, one before the window, a prompt that does not, and a tool call that does
- **WHEN** the operator searches for "deploy"
- **THEN** the three prompts in the window are found newest first, each with its host and session, a page at a time
- **AND** with a host, only that host's are found, and `%` and `_` in the query match only themselves
- **AND** a prompt sent again with changed text is found by its new text only, and a later page reads the first page's window

#### Scenario: Each search is audited with its query

- **GIVEN** an operator holding `agent_prompt.read`
- **WHEN** they search for " Deploy "
- **THEN** prompt read is asked for, and audited as, an `agent_prompt_search` named "Deploy"
- **AND** without `agent_prompt.read` the search is refused and nothing is read

### Requirement: A prompt search is bounded

A prompt search SHALL read a window of at most 30 days, a week ending now when none is named, and SHALL reject a wider, inverted or negative window with `bad_window` and a query that is not UTF-8, shorter than 3 characters or longer than 255 bytes with `invalid_query`, reading nothing. It SHALL read only the window's prompts.

#### Scenario: A search is bounded

- **GIVEN** a search with no window
- **WHEN** it is made
- **THEN** it reads the week ending now
- **AND** a window of 31 days, an inverted one, a negative bound, no query, a two-character query, a 256-byte one and one that is not UTF-8 are each rejected without a read, while one of exactly 30 days is read
- **AND** beside sixty thousand other events, a search for one prompt reads fewer than half the rows
