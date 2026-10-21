## ADDED Requirements

### Requirement: Services under their app

The session view and the host graph's logical tree SHALL list a process the read links to an app as a `responsible_process` under that app, marked as a service rather than as lineage or an inferred launch. Such a row SHALL never fold into an identical sibling.

#### Scenario: A service hangs under the app responsible for it

- **GIVEN** a session that opened TextEdit and an XPC service the read links to TextEdit as `responsible_process`
- **WHEN** the session view lists the processes
- **THEN** the service is listed under TextEdit, badged as a service, with a tooltip saying macOS records the app as responsible for it
