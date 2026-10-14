## ADDED Requirements

### Requirement: AI agent session view badges

The session view SHALL read its header from the session's record and SHALL say when the session began before the retention cutoff. It SHALL badge each alert on the process it was raised on and on its command, and SHALL never hide such a process as shell plumbing. It SHALL badge a process that runs as a different user from its creator, and SHALL credit each alert's rule author beside it. A session of more than 10 commands SHALL open with only its alerted commands expanded.

#### Scenario: The header reads the session's record

- **GIVEN** a session whose record names its agent pid and project
- **WHEN** the operator opens the session's view
- **THEN** the header shows the agent pid and the record's project

#### Scenario: Retention is reported when the session began before it

- **GIVEN** a session first seen before the retention cutoff the process read reports
- **WHEN** the operator opens the session's view
- **THEN** it says the session began before the cutoff and that processes finished before then have been removed
- **AND** a session first seen after the cutoff carries no such notice

#### Scenario: Alerts are badged where they were raised

- **GIVEN** a critical alert raised on a process of a session's first tool call
- **WHEN** the operator opens the session's view
- **THEN** the process carries the alert's badge, linking to the alert
- **AND** the command's header carries "1 alert" in the critical style

#### Scenario: An alerted process is never hidden as plumbing

- **GIVEN** an alert raised on a tool call's wrapper shell
- **WHEN** the operator opens the session's view with shell plumbing hidden
- **THEN** the wrapper shell is listed with its alert, and its commands under it

#### Scenario: A long session opens on its alerts

- **GIVEN** a session of 11 tool calls with an alert in the seventh
- **WHEN** the operator opens the session's view
- **THEN** only the seventh call is expanded

#### Scenario: A privilege change is badged

- **GIVEN** a tool call that ran `sudo id`, where setuid `sudo` runs as root under the agent's uid-501 shell and `id` runs as root under `sudo`
- **WHEN** the operator opens the session's view
- **THEN** `sudo` carries a "runs as root" badge and `id`, which runs as the same user as its creator, carries none
