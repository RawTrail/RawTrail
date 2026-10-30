## ADDED Requirements

### Requirement: Sessions list bypass filter

The Agent sessions page SHALL offer a filter, labelled "Ran in bypass mode (or forked from a session that did)", that asks the server for only the sessions that ran with permissions bypassed, and SHALL badge each such session's row "Bypass permissions", the badge's title saying a fork of a bypassed session carries it too.

#### Scenario: The list narrows to sessions that ran bypassed

- **GIVEN** two sessions on a host, one whose messages were written with permissions bypassed
- **WHEN** the operator lists the host's sessions and adds the bypass filter
- **THEN** only the bypassed session's row is badged, and with the filter the list holds only it
