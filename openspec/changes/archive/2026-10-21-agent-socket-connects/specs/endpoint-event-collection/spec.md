## ADDED Requirements

### Requirement: Agent socket connects

The extension SHALL report each connect by a process in an AI agent's subtree to a Unix-domain socket as a `file_activity` row with operation `connect` and the socket file's path, folded and bounded as the subtree's file operations are. It SHALL NOT report connects by processes outside agent subtrees, and SHALL NOT attribute the daemon's own activity to the session.

#### Scenario: A session's connect to a daemon's socket is reported

- **GIVEN** a process an AI agent started connects to `/var/run/docker.sock` three times within one second
- **WHEN** the extension folds the window
- **THEN** it sends one `file_activity` row with operation `connect`, path `/var/run/docker.sock` and `coalesced_count` 3, apart from any other operation on that path
