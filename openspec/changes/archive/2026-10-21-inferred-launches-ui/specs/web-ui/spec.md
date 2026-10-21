## ADDED Requirements

### Requirement: Inferred launches in the UI

The session view SHALL list a process launchd started for a call's `open -a` or `launchctl submit` under that launcher, marked as an inferred launch. The host graph SHALL offer a logical tree, when the read carries links, that hangs each launched process under its launcher with a distinct edge and label, the strict tree remaining the default. An inferred launch SHALL never be drawn as parentage.

#### Scenario: A launched process hangs under its launcher

- **GIVEN** a forest where launchd started a process, with a child of its own, for an `open` exec, and the read links the two
- **WHEN** the links are applied
- **THEN** the launched process and its child are under the `open` in fork order, the caller's forest is unchanged, and a link whose ends are absent or that would hang a process under its own descendant is not applied

#### Scenario: A session lists a launch under the call that asked

- **GIVEN** a Claude Code session whose call ran `open -a TextEdit`, and TextEdit, a root carrying the session's markers, linked to that `open`
- **WHEN** the operator opens the session's view
- **THEN** TextEdit is listed under the call's `open` with an "inferred launch" badge, and the session shows one call, not two

#### Scenario: The host graph toggles a logical tree

- **GIVEN** a host graph whose read links a launched process to its `open`
- **WHEN** the operator turns on "Logical tree", then off
- **THEN** the launched process is drawn under the `open` with a dashed edge and an "inferred launch" label, then back where its parentage puts it
- **AND** a read without links offers no such toggle
