## ADDED Requirements

### Requirement: A contained host's content filter is required

While the host is network contained, the agent SHALL treat a decoded provider report that does not name the content filter as the content filter stopped, for health, for the self-heal and for the record of provider transitions. A report that did not decode SHALL be left as it is, and an uncontained host's report SHALL be read as the extension sent it.

#### Scenario: A contained host whose filter never starts reads unhealthy

- **GIVEN** a contained host whose content filter's NetworkExtension configuration is switched off, so the filter never starts and the extension reports only its DNS proxy
- **WHEN** the agent reads the report
- **THEN** health shows the content filter stopped and the network extension unhealthy, rather than healthy
- **AND** the self-heal re-enables the filter, and a filter that was running and then went missing is recorded as a stop

#### Scenario: An uncontained host's missing filter is not a fault

- **GIVEN** a host that is not contained, whose extension reports only its DNS proxy
- **WHEN** the agent reads the report
- **THEN** no content filter is claimed and nothing is repaired, as before
