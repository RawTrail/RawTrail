## MODIFIED Requirements

### Requirement: Snapshot is the source of truth for decisions

The extension SHALL keep an in-memory snapshot of the active policy, indexed by `(rule_type, identifier)`, and persist it to `/var/db/io.rawtrail/application-control.json` by writing a temporary file and renaming it. It SHALL accept a snapshot for the same `policy_id` only when it is ahead of the active one, ordered by `policy_epoch` (absent counts as `0`) and then `policy_version`, and SHALL otherwise keep the active snapshot without writing. The watched-path set is ordered the same way.

#### Scenario: An incoming snapshot replaces the prior one atomically

- **GIVEN** the extension is already running with an applied snapshot at version `V` and epoch `E`
- **WHEN** it receives a new snapshot at version `V+1` with an epoch later than `E`
- **THEN** the in-memory snapshot is the `V+1` snapshot immediately after acceptance
- **AND** the on-disk file reflects the same version
- **AND** no exec is evaluated against a partial snapshot during the swap

#### Scenario: Extension restart restores the last applied snapshot

- **GIVEN** the extension has previously applied a snapshot at version `V` to disk
- **WHEN** the extension restarts
- **THEN** the in-memory snapshot is the version `V` snapshot from disk

#### Scenario: A stale snapshot is rejected

- **GIVEN** the extension's current snapshot is at version `V` and epoch `E`
- **WHEN** a snapshot for the same policy is delivered whose version is `<= V` AND whose epoch is `<= E`
- **THEN** the extension keeps its current snapshot and performs no disk write

#### Scenario: A pre-restore snapshot is refused

- **GIVEN** the extension's current snapshot was saved after a server database restore, at a version lower than before the restore
- **WHEN** a snapshot issued before the restore is delivered for the same policy, with a higher version and an earlier epoch
- **THEN** the extension keeps its current snapshot and performs no disk write

#### Scenario: A version regression with a newer epoch re-syncs instead of freezing

- **GIVEN** the extension's current snapshot is at version `V` and epoch `E` (for example `V=25` persisted before a server database restore)
- **WHEN** a snapshot for the same policy is delivered whose version is lower than `V` but whose epoch is greater than `E` (the post-restore mutation stamped a fresh `updated_at`)
- **THEN** the extension accepts the snapshot and the in-memory and on-disk forms reflect the delivered lower version
- **AND** the delivered ruleset enforces immediately rather than the host staying frozen on the stale ruleset
