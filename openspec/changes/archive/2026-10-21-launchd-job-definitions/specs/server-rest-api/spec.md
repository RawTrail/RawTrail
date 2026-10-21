## ADDED Requirements

### Requirement: Launches by job definition

A tree read SHALL link a `launchctl load` or `bootstrap` exec carrying `launchd_jobs` to the program launchd started from each xpcproxy row naming one of their labels within 15 seconds, one launch per job. A bootstrap SHALL be held to the uid its `gui/` or `user/` domain names, and a load to its caller's unless the caller is root.

#### Scenario: A load links each job it defined

- **GIVEN** a session's `launchctl load` of two plists defining two labels, and launchd starting an unrelated job between the two it loaded
- **WHEN** the session's tree is read
- **THEN** the load is linked to both jobs' programs, not to launchd's xpcproxy rows, and not to the unrelated job
