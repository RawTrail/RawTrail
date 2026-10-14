## ADDED Requirements

### Requirement: Inferred launch links

A window, session or story tree read SHALL link an `open -a <App>`, `open <path>.app` or `launchctl submit ... -- <program>` exec to the first process launchd started (not adopted) within 5 seconds of it that runs what it named, earliest ask first, each launched process once, for at most 50 launchers per read. A session or story read SHALL include a linked process with its descendants and report truncation when they are cut. A link SHALL NOT change parentage.

#### Scenario: A session read links and includes what its launchers started

- **GIVEN** an agent session whose tool calls ran `open -a TextEdit` and `launchctl submit -- /bin/sleep 61`, after which launchd started TextEdit, carrying the session's markers, and `/bin/sleep`, carrying none, each through an xpcproxy row
- **WHEN** the session is read
- **THEN** the response links the `open` to TextEdit and the `launchctl` to `/bin/sleep`
- **AND** `/bin/sleep` and what it started are in the forest, without the xpcproxy row, and TextEdit is listed once

#### Scenario: A launcher is matched to what it named

- **GIVEN** a launchd child that runs something else within the window, a process of the agent's own running the same program, the same program started by launchd before the ask, a daemon launchd adopted rather than started, a second `open -a TextEdit` before the app started, a later `open -a TextEdit` whose own launch follows it, and launchers that name a bundle id or a plist
- **WHEN** the tree is read
- **THEN** each launcher is linked to its own launch, and none of the other processes, nor the launchers naming nothing a path matches, are linked

#### Scenario: A window read links only what it holds

- **GIVEN** a window that holds a `launchctl submit` but ends before launchd started its program
- **WHEN** the window is read
- **THEN** no link is made for it and no row outside the window is added
