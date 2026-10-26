## ADDED Requirements

### Requirement: A watched-path push before the connection is replayed

The agent SHALL send the newest watched-path set that found no connection to the system extension as soon as a connection is published, newest by epoch and then version as the extension orders them, not by arrival. The push's command still fails, because it did not reach the extension when it was made. The set is not otherwise sent again for hours, and a freshly installed host is pushed it before its extension is approved.

#### Scenario: A push before the extension connects is replayed

- **GIVEN** the agent has no connection to the system extension
- **AND** it is asked to push the watched paths
- **WHEN** a connection to the extension is published
- **THEN** the agent sends the extension those watched paths
- **AND** the push's command was reported failed when it was made

#### Scenario: A later push replaces an earlier one

- **GIVEN** the agent has no connection and has been asked to push two watched-path sets in turn, the second the newer
- **WHEN** a connection is published
- **THEN** the agent sends only the newer set

#### Scenario: An older set arriving later does not replace it

- **GIVEN** the agent has no connection and is asked to push a newer set and then an older one, as a poll delivers them newest first
- **WHEN** a connection is published
- **THEN** the agent sends only the newer set

#### Scenario: A replayed push is sent once

- **GIVEN** a watched-path push was replayed on a connection
- **WHEN** the connection drops and another is published
- **THEN** the agent does not send that push again
