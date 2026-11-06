## MODIFIED Requirements

### Requirement: Notarization and stapling

A real release build SHALL submit the final installer package to the Apple notary, wait for acceptance, and staple the notarization ticket to the package so that Gatekeeper can validate the package offline at install time. A failed fetch of the ticket SHALL be retried a bounded number of times before the build fails. A package that has not been stapled MUST NOT be published.

#### Scenario: Released package is stapled

- **GIVEN** a notarized installer package
- **WHEN** the build script completes
- **THEN** the package carries a stapled notarization ticket
- **AND** offline Gatekeeper validation of the package succeeds

#### Scenario: A failed ticket fetch is retried

- **GIVEN** a package the notary accepted, whose ticket fetch fails four times and then succeeds
- **WHEN** the build script staples it
- **THEN** the fifth attempt staples the ticket and the package is validated
- **AND** a fetch that fails five times fails the build without validating the package
