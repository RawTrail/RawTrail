## ADDED Requirements

### Requirement: The credential guard is edited in detection tuning

The Detection tuning page SHALL include an AI agent credential guard section showing the mode, each protected credential in plain words with its path, coverage and allowed tools, which are built in, and the last save. An operator with `detection_config.write` SHALL edit a draft of the whole policy and save it with a reason through `PUT /api/v1/detection-config/credential-guard`, naming the version the draft started from.

#### Scenario: The console shows the policy in plain words

- **GIVEN** a stored policy in detect mode over the built-in SSH key prefix and GitHub CLI token
- **WHEN** an operator opens Detection tuning
- **THEN** the section shows detect mode, each credential by name with its path in each home, what it covers and the tools allowed to read it, both marked built in, and who saved it last

#### Scenario: A credential being edited is not dropped

- **GIVEN** an operator editing a credential, which takes it out of the list into the form
- **WHEN** they clear the form's fields
- **THEN** saving the policy stays unavailable until the entry is saved back or the edit is cancelled, which puts it back where it was

#### Scenario: A changed policy is saved with a reason and pushed

- **GIVEN** an operator with `detection_config.write`
- **WHEN** they switch to protect, add AWS credentials allowed to one team, edit the GitHub CLI token's tools, and save with a reason
- **THEN** the whole policy is sent with the reason and the version the draft started from, and the section reports the saved version and the push
