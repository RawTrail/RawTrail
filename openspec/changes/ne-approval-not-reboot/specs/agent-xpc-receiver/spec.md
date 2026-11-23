## MODIFIED Requirements

### Requirement: Distinct signal when a sysext upgrade leaves the NE registration stale

When the agent cannot establish the network-extension XPC connection while a previous network-extension version is pending removal on reboot and the new version is activated and enabled, the agent SHALL signal that a reboot is required, in its log and as the extension's health, once per stale episode until the next connection. Otherwise it SHALL keep the generic connect-failure warning.

#### Scenario: NE connect keeps failing while a prior version waits to uninstall on reboot

- **GIVEN** a system-extension upgrade has been staged and the previous network-extension version is waiting to uninstall on reboot
- **AND** the new network-extension version is activated and enabled
- **AND** the agent's network-extension receiver cannot establish its XPC connection
- **WHEN** the receiver has failed to connect past the grace threshold
- **THEN** the agent emits the distinct reboot-required signal, in its log and as the network extension's health, so an operator sees it in the console and not only in the agent log
- **AND** it does not repeat the signal on every subsequent failed attempt within the same episode

#### Scenario: A not-yet-approved network extension keeps the generic warning

- **GIVEN** no previous network-extension version is pending removal on reboot
- **AND** the agent's network-extension receiver cannot establish its XPC connection
- **WHEN** the receiver fails to connect
- **THEN** the agent emits the generic connect-failure warning and not the upgrade-reboot signal

#### Scenario: Awaiting approval is not a reboot

- **GIVEN** previous network-extension versions waiting to uninstall on reboot, as an upgrade leaves them until the next restart
- **AND** the new version is activated and waiting for the person to approve it
- **WHEN** the agent checks whether a reboot finishes the upgrade
- **THEN** it reports no reboot, so the receiver keeps the generic warning and the extension stays `never_connected`, since approving it is what connects the agent

#### Scenario: Switched off is not a reboot

- **GIVEN** a previous network-extension version waiting to uninstall on reboot
- **AND** the new version is activated but switched off
- **WHEN** the agent checks whether a reboot finishes the upgrade
- **THEN** it reports no reboot, since a restart does not switch the extension back on
