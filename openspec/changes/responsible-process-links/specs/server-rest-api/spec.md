## ADDED Requirements

### Requirement: Responsible process links

A story or session tree read SHALL link each process it holds that is responsible for itself to every process launchd started that names it, by pid and generation, as responsible within its lifetime, and SHALL add each such process with its descendants as an extra root. A window read SHALL link only what it holds. A read SHALL report a cut when an app has more such processes than are read.

#### Scenario: A session read links and includes the services of its apps

- **GIVEN** a session holding TextEdit, launched by launchd and responsible for itself, and two XPC services launchd started that name TextEdit's pid and generation
- **AND** decoys: a service naming TextEdit's pid under another generation, one naming another app, one started after TextEdit exited, and one naming the SSH login a shell command ran under
- **WHEN** the session's tree is read
- **THEN** the two services are linked to TextEdit as `responsible_process` and added with what they started, and no decoy is linked or added
