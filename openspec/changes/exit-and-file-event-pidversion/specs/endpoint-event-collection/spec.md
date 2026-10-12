## ADDED Requirements

### Requirement: Exit and file event process generation

The `exit` event SHALL carry the exiting process's `pidversion`. The `open`, `file_rename`, `file_truncate` and `file_delete` events SHALL carry the acting process's `pidversion`. Each value SHALL equal the `pidversion` of the exec event that started the running image, or of the fork event when the process never exec'd. The field is optional on the wire.

#### Scenario: An exit names the generation that exited

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs and later exits
- **THEN** the `exit` event's `pidversion` equals the `pidversion` on that process's exec event

#### Scenario: A file event names the generation that acted

- **GIVEN** the endpoint event capture is running
- **WHEN** a process execs and then truncates, deletes, renames or writes a watched file
- **THEN** the file event's `pidversion` equals the `pidversion` on that process's exec event

#### Scenario: A payload without a generation keeps its shape

- **GIVEN** an exit or file payload built without a `pidversion`
- **WHEN** it is serialized
- **THEN** it has no `pidversion` key
