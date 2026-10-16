## ADDED Requirements

### Requirement: Launches by label and bundle id

A tree read SHALL link a `launchctl kickstart <domain>/<label>` or `launchctl start <label>` exec to the program launchd started from the xpcproxy row naming that label, and an `open -b <bundle id>` exec to the launchd child whose code-signing identifier is the bundle id, ignoring case, either starting within 15 seconds of the exec.

#### Scenario: A label or bundle id names its launch exactly

- **GIVEN** `kickstart -k gui/501/<label>`, `start <label>` and `open -b com.apple.TextEdit` execs
- **WHEN** each is matched against launchd children
- **THEN** the xpcproxy row naming the label, or the app signed `com.apple.textedit`, matches, and another label, another identifier or an unsigned app does not

#### Scenario: A session read links a kickstart, a start and an open by bundle id

- **GIVEN** a session that ran `kickstart -k` on a job launchd restarted 8.3 s later, `start` on another job, and `open -b com.apple.TextEdit`, among another job's start, the same label started past the window, and an app signed with another identifier
- **WHEN** the session's tree is read
- **THEN** each command is linked to its own program or app, the job's xpcproxy row is not listed, and no decoy is linked
