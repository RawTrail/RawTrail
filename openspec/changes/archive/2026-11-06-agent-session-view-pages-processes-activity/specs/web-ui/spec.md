## ADDED Requirements

### Requirement: Agent session view pages its processes and activity

The session view's Processes SHALL offer the session's next processes on request, listing each under the process that created it when an earlier page read it. Each Activity class SHALL offer its next rows on request until none remain. A read that fails SHALL say so and SHALL offer the page again.

#### Scenario: The rest of the processes come on request

- **GIVEN** a session with more processes than its tree's first page
- **WHEN** the operator asks for the next processes
- **THEN** they are listed, each under its creator when an earlier page read it, and none is offered once the last page is read

#### Scenario: The rest of an activity list comes on request

- **GIVEN** a session whose DNS names outnumber the rollup's list
- **WHEN** the operator asks for the next rows, page by page
- **THEN** each page's rows are listed after the ones before, until the last, after which none is offered and the cut notice is gone
