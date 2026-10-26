## ADDED Requirements

### Requirement: A watched-path push before the connection is replayed

The agent SHALL send the most recent watched-path push that found no connection to the system extension as soon as a connection is published. The push's command still fails, because it did not reach the extension when it was made. The set is not otherwise sent again for hours, and a freshly installed host is pushed it before its extension is approved.

#### Scenario: A push before the extension connects is replayed

- **GIVEN** the agent has no connection to the system extension
- **AND** it is asked to push the watched paths
- **WHEN** a connection to the extension is published
- **THEN** the agent sends the extension those watched paths
- **AND** the push's command was reported failed when it was made

#### Scenario: A later push replaces an earlier one

- **GIVEN** the agent has no connection and has been asked to push two watched-path sets in turn
- **WHEN** a connection is published
- **THEN** the agent sends only the later set

#### Scenario: A replayed push is sent once

- **GIVEN** a watched-path push was replayed on a connection
- **WHEN** the connection drops and another is published
- **THEN** the agent does not send that push again
