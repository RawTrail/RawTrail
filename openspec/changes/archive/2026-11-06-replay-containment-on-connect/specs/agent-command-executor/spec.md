## ADDED Requirements

### Requirement: Containment sent before the extension connects is replayed

The agent SHALL send the newest containment command that found no connection to the network extension as soon as one is published, newest by epoch, then version, then the reachable set's version, and SHALL rebuild the document at that moment. The command still fails, because it did not reach the extension when it was made. A kept command the extension already holds or has been overtaken by SHALL NOT be sent.

#### Scenario: A command before the extension is replayed

- **GIVEN** the agent has no connection to the network extension
- **AND** a containment command is executed
- **WHEN** a connection to the extension is published
- **THEN** the agent sends the extension that state, with the lifeline looked up at that moment
- **AND** the command was reported failed when it was made

#### Scenario: The newest command is the one replayed

- **GIVEN** the agent has no connection and executes a containment and a release, in either order
- **WHEN** a connection is published
- **THEN** the agent sends only the command that is newest by the extension's order

#### Scenario: An overtaken command is not replayed

- **GIVEN** a containment command kept for lack of a connection
- **AND** the extension has since confirmed a later state
- **WHEN** a connection is published
- **THEN** the agent sends nothing
