## ADDED Requirements

### Requirement: Process search lineage filters

The fleet search page SHALL offer Agent, Agent session, Story root, Descendant of, Argument and Since filters on processes, kept in the URL like the others. Since SHALL reach back an hour, a day, a week or 30 days from when the search first read a page, every page reading the same window.

#### Scenario: Every curl any Claude Code session ran this week is one search

- **GIVEN** the search page with Path `curl`, Agent Claude Code and Since Last week
- **WHEN** it searches
- **THEN** one request asks for `path=curl`, `agent=claude-code` and a `from` a week before the search ran

#### Scenario: A session, story or ancestor chip narrows the search

- **GIVEN** the search page with Agent session, Story root, Descendant of and Argument set
- **WHEN** it searches
- **THEN** each is passed to the endpoint as it was entered
