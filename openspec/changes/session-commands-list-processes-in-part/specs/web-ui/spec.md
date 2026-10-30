## ADDED Requirements

### Requirement: Session view reads a large command whole

When a command's processes are listed in part, the session view SHALL say so and offer "Show all N processes", which reads the command whole and lists it in place; a failed read SHALL say so and leave the offer to retry.

#### Scenario: A command listed in part is read whole on request

- **GIVEN** a command of 26 processes of which the page lists 6
- **WHEN** the operator chooses "Show all 26 processes"
- **THEN** the command is read whole and its processes are listed, the first 20 and the rest on request

#### Scenario: A failed read of a whole command says so

- **GIVEN** a command listed in part whose whole read fails
- **WHEN** the operator chooses "Show all N processes"
- **THEN** the view says the processes could not be loaded, keeps what it listed, and offers the read again
