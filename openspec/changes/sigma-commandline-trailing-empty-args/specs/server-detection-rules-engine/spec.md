## ADDED Requirements

### Requirement: The command line ends with the last non-empty argument

The system SHALL build an exec event's Sigma `CommandLine` by joining its arguments with single spaces, leaving out the empty arguments at the end of the argument vector. An exec whose arguments are all empty SHALL supply no `CommandLine`. A separator is not text the process received, so a joined trailing space must not read as an argument that ends in one.

#### Scenario: A trailing empty argument adds no space

- **GIVEN** an exec whose argument vector is `pidinfo --git-state /Users/me/repo 4 1 ""`
- **WHEN** a process-creation rule reads its `CommandLine`
- **THEN** the value is `pidinfo --git-state /Users/me/repo 4 1`, with no trailing space
- **AND** a rule matching `CommandLine|endswith: ' '` does not match it

#### Scenario: A space inside the last argument is kept

- **GIVEN** an exec whose last argument is `invoice.txt ` (ending in a space), followed by an empty argument
- **WHEN** a rule matching `CommandLine|endswith: ' '` is evaluated against it
- **THEN** it matches, because the space is in the argument

#### Scenario: All-empty arguments supply no command line

- **GIVEN** an exec whose only argument is empty
- **WHEN** a rule reads its `CommandLine`
- **THEN** the field is absent, as for an exec with no arguments

#### Scenario: An empty middle argument keeps its spaces

- **GIVEN** an exec whose argument vector is `tool "" -d`
- **WHEN** a rule reads its `CommandLine`
- **THEN** the value is `tool  -d`, with the empty argument's separators in place
