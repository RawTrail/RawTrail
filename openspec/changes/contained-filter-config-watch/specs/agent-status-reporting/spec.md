## ADDED Requirements

### Requirement: A contained host's filter configuration is checked

While the host is network contained, the agent SHALL read the content filter's configuration through the host app's `filter-status` at least every 30 seconds, and SHALL treat a configuration that is switched off as the content filter stopped, whatever the extension reports, for health, the self-heal and the record of provider transitions. An uncontained host's configuration SHALL not be read, and a read that fails SHALL change nothing.

#### Scenario: A filter orphaned with its configuration off reads stopped

- **GIVEN** a contained host whose filter configuration was switched off while the filter was starting, so the extension still reports the filter running though no flows reach it
- **WHEN** the agent reads the configuration
- **THEN** health shows the content filter stopped and the self-heal re-enables it
- **AND** once the configuration reads enabled again, the extension's report is read as it was sent

#### Scenario: An uncontained host's configuration is not read

- **GIVEN** a host that is not contained
- **WHEN** the check comes due
- **THEN** the host app is not run, and the extension's report is read as it was sent
