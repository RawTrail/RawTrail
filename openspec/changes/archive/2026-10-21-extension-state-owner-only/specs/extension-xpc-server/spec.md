## ADDED Requirements

### Requirement: Persisted extension state is readable only by root

The extensions SHALL persist every pushed document in an owner-only directory (mode 0700) as an owner-only file (mode 0600), so a local user who is not root cannot read what the EDR watches, blocks or allows while a host is contained. A directory open to group or other SHALL be tightened by the next write.

#### Scenario: A pushed document is written owner-only

- **GIVEN** the state directory is mode 0755 and holds a mode 0644 file, as an earlier install left them
- **WHEN** an extension persists a document to that file
- **THEN** the directory is mode 0700 and the file is mode 0600
- **AND** the file holds the new document
