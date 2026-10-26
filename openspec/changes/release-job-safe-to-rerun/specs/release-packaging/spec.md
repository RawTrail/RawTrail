## ADDED Requirements

### Requirement: A release is published only when complete and its build can be repeated

The release pipeline SHALL create the GitHub release as a draft and publish it as its last step, after every artifact and every Sigstore bundle is attached and the provenance is attested. A run SHALL reuse an existing draft for its tag, replacing every artifact together, and SHALL refuse a release that is already published. The cosign install SHALL be retried before it fails the run.

#### Scenario: A failed run leaves only a draft

- **GIVEN** a release tag whose pipeline fails after the release is created
- **WHEN** the run ends
- **THEN** the release is a draft that no one but the maintainers can see

#### Scenario: A re-run reuses the draft

- **GIVEN** a draft release left by a failed run of the same tag
- **WHEN** the pipeline is re-run
- **THEN** it replaces every artifact and bundle on that draft instead of failing because the release exists

#### Scenario: A published release is not overwritten

- **GIVEN** a release that is already published
- **WHEN** the pipeline runs for its tag
- **THEN** it fails without uploading, because replacing published bytes is not a repair

#### Scenario: A complete release is published

- **GIVEN** a run in which every artifact and bundle is uploaded and the provenance attested
- **WHEN** the last step runs
- **THEN** the draft is published, as the latest release only when the tag is not a prerelease

#### Scenario: The cosign install is retried

- **GIVEN** a transient failure while installing cosign
- **WHEN** the install step fails
- **THEN** it is attempted again, up to three attempts, before the run fails
