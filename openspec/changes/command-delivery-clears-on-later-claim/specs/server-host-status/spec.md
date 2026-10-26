## ADDED Requirements

### Requirement: A host that takes commands again stops being reported as not taking them

The system SHALL stop counting an expired command toward the not-taking-commands condition once the host has claimed a later command. Expiries after the most recent claim SHALL still count. A host back from days offline has expired commands with nothing wrong, and reporting it for the rest of the window would assert in the present tense something that is no longer true.

#### Scenario: A later claim clears the condition

- **GIVEN** a host with commands that aged out undelivered
- **AND** the host has since claimed a later command, whether it then completed, failed or is still in flight
- **WHEN** its health is read
- **THEN** no not-taking-commands condition is reported

#### Scenario: Only expiries after the latest claim count

- **GIVEN** a host that claimed a command after one expiry and has had another command age out since
- **WHEN** its health is read
- **THEN** the condition counts only the expiry after the claim
