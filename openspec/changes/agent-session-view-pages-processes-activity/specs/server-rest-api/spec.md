## ADDED Requirements

### Requirement: A tree read reports its links cut

A process tree read SHALL set `links_truncated` when what it added through its links, a launched program's descendants or an app's services, was cut at its bound, and SHALL also set `truncated` then. A client joining a scoped read's pages SHALL keep the read partial while any page reported it, since a later page does not read those processes again.

#### Scenario: A launched program cut is told apart from paging

- **GIVEN** a session whose launched program has more descendants than a read adds
- **WHEN** the session's tree is read whole in one page
- **THEN** the read sets `links_truncated` and `truncated`, and names no next page
