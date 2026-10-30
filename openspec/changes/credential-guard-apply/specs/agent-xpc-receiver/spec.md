## ADDED Requirements

### Requirement: A credential guard push before the connection is replayed

A credential guard policy that finds no extension connection SHALL fail its command and SHALL be held, newest by epoch then version, and sent once when a connection is published, as a watched-path set is. The two pushes SHALL be held apart, so neither displaces the other.

#### Scenario: A policy pushed before connecting is replayed

- **GIVEN** two policies pushed, newest first, and a watched-path set, all before the extension connects
- **WHEN** the extension connects
- **THEN** it is sent the newer policy and the set, once each
- **AND** a later connection is not sent them again
