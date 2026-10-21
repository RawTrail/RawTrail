## ADDED Requirements

### Requirement: Forked child inherits its resolved creator's image

When a fork's creator is resolved by `(parent_pid, parent_pidversion)`, the forked child SHALL inherit that creator row's path and, if the creator's image was in force at the fork, its signing identity. A fork whose creator was not resolved SHALL inherit the image of the newest generation of the parent's pid to have forked by the child's fork.

#### Scenario: A child runs the image of the creator it names

- **GIVEN** zsh, generation 1001 of pid 500, and python3, which reused pid 500 and is already stored while zsh's exit is not
- **WHEN** a child's fork names pid 500 and generation 1001, stamped after python3 forked
- **THEN** the child's path and signing identity are zsh's, not python3's
- **AND** a child stamped before zsh's image was applied gets zsh's path without its identity

#### Scenario: A child that names no creator inherits as before

- **GIVEN** the same host
- **WHEN** a child's fork names pid 500 without a generation
- **THEN** the child inherits python3's image, the newest generation of the pid to have forked by the child's fork
