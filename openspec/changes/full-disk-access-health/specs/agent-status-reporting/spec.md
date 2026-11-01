## ADDED Requirements

### Requirement: A missing Full Disk Access grant is reported

The security extension SHALL record why macOS refused it an Endpoint Security client before it exits, and remove the record once a client is granted. The agent SHALL report a refusal for lack of permission as the security extension's condition `full_disk_access_missing`, kept through reconnect attempts until a connect, and SHALL report its own grant as the `agent_full_disk_access` component.

#### Scenario: The extension records why its client was refused

- **GIVEN** macOS refuses the extension an Endpoint Security client
- **WHEN** the extension exits
- **THEN** its record names the refusal (`not_permitted` for a missing Full Disk Access grant) and when, in the shape the agent reads

#### Scenario: A refused extension reports the missing grant

- **GIVEN** an extension whose recorded refusal is `not_permitted`, and that has not connected
- **WHEN** the agent reports its health, including after failed reconnect attempts
- **THEN** the security extension is unhealthy with reason `full_disk_access_missing` and a message naming where to grant it; another refusal, or none, leaves its condition as it was, and a later connect clears it

#### Scenario: The agent reports its own grant

- **GIVEN** an agent that can, then cannot, read the system privacy database
- **WHEN** it reports its health
- **THEN** `agent_full_disk_access` is healthy, then unhealthy with reason `full_disk_access_missing`; a database it cannot find changes nothing
