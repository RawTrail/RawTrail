## ADDED Requirements

### Requirement: A large tie is joined in linear time

Pairing a session's commands with its tool calls SHALL cost, for a tie of n identical calls, O(n) over all the commands it is offered rather than O(n) for each, so the largest tie a session read takes is joined well within the server's write timeout, with the same labels.

#### Scenario: The largest tie a session read takes is joined at once

- **GIVEN** 20,000 identical Bash calls in one turn and 20,500 commands that ran the same text after it
- **WHEN** the commands are paired with the calls
- **THEN** 20,000 commands are labelled with the one shared tie, the rest are left unlabelled, and the join takes well under a second
