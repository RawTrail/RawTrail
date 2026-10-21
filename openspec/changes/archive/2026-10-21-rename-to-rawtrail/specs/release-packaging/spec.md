## MODIFIED Requirements

### Requirement: Final artifact naming

The release pipeline SHALL produce a final installer artifact at `dist/rawtrail-<version>.pkg` where `<version>` is derived from the release tag. When the tag contains characters not safe in a filename, those characters MUST be replaced with `-` so the resulting path is a single regular file.

#### Scenario: Versioned package name

- **GIVEN** a release tag `v1.2.3`
- **WHEN** the build completes
- **THEN** the final artifact path is `dist/rawtrail-v1.2.3.pkg`

#### Scenario: Tag with a path-separator character

- **GIVEN** a release tag `release/v1.2`
- **WHEN** the build completes
- **THEN** the final artifact path is `dist/rawtrail-release-v1.2.pkg`

### Requirement: Installation activates the system extensions

The released package SHALL ship a LaunchAgent at `/Library/LaunchAgents/io.rawtrail.activate.plist` that runs the host app's `activate` subcommand in the logged-in user's GUI session, and the install scripts SHALL start it, so an MDM-managed host's extensions reach `activated enabled` without operator interaction. Activation MUST NOT be submitted from the root postinstall or the agent daemon, MUST NOT fail the install, and uninstall MUST remove the LaunchAgent.

#### Scenario: Install with a user logged in activates immediately

- **GIVEN** an MDM-managed host with the system-extension profile installed and a user logged in at the console
- **WHEN** the pkg installs
- **THEN** the postinstall bootstraps the activation LaunchAgent into the console user's GUI domain
- **AND** both extensions reach `activated enabled` without any user interaction

#### Scenario: Install at the loginwindow activates at next login

- **GIVEN** an MDM-managed host with the system-extension profile installed and no user logged in
- **WHEN** the pkg installs and a user later logs in
- **THEN** the LaunchAgent runs the host app's `activate` at login and both extensions reach `activated enabled` without any user interaction

#### Scenario: Uninstall removes the activation LaunchAgent

- **GIVEN** a host where the released package is installed
- **WHEN** the operator runs the uninstall script
- **THEN** the activation LaunchAgent is booted out of the GUI domain and its plist is removed

### Requirement: Uninstall path is deliverable

The released package SHALL include an uninstall script that removes the agent, the host app and both system extensions without the original package. It SHALL deactivate the extensions through the host app's `deactivate` subcommand in the console user's GUI session before removing the app bundle, never through `systemextensionsctl`, and SHALL report the outcome from live system-extension state. While an extension stays active it MUST NOT claim success and MUST keep the host app bundle.

#### Scenario: Operator runs the uninstall script

- **GIVEN** a host on which the released package was installed
- **WHEN** the operator runs the bundled uninstall script as root
- **THEN** the script stops and unloads the agent's launch daemon, removes the activation LaunchAgent, deactivates the system extensions through the host app, and removes the agent's binaries and runtime state

#### Scenario: Uninstall deactivates both extensions via the host app

- **GIVEN** an unmanaged host with both RawTrail system extensions active and a user logged in at the console
- **WHEN** the operator runs the uninstall script as root
- **THEN** the script submits `edr deactivate` in the console user's GUI session before removing the app bundle, so both the Endpoint Security and Network extensions are removed (or staged for removal on reboot)
- **AND** the script does not invoke `systemextensionsctl deactivate`

#### Scenario: Uninstall on an MDM-managed host reports the profile must be removed

- **GIVEN** an MDM-managed host whose system extensions were approved by the `io.rawtrail.profile.system-extension` profile
- **WHEN** the operator runs the uninstall script as root
- **THEN** the host-app deactivation is refused by macOS (authorizationRequired) and the extensions stay active
- **AND** the script keeps the host app bundle, does not claim a clean removal, and tells the operator to remove the system-extension profile via their MDM to complete removal

### Requirement: System extensions present recognizable display names

The packaged Endpoint Security and Network system extensions SHALL each carry a `CFBundleDisplayName` that identifies it as a RawTrail component, so the System Settings entries an operator acts on during a manual install are attributable to RawTrail rather than a generic label. The packaging build MUST fail rather than produce a package whose staged extensions carry any other display name.

#### Scenario: Security extension shows a recognizable name

- **GIVEN** a packaging build that stages the Endpoint Security extension into the host app
- **WHEN** the build inspects the staged `io.rawtrail.securityextension.systemextension` bundle
- **THEN** its `CFBundleDisplayName` is "RawTrail Security Extension"
- **AND** the build fails if the display name is anything else

#### Scenario: Network extension shows a recognizable name

- **GIVEN** a packaging build that stages the Network Extension into the host app
- **WHEN** the build inspects the staged `io.rawtrail.networkextension.systemextension` bundle
- **THEN** its `CFBundleDisplayName` is "RawTrail Network Extension"
- **AND** the build fails if the display name is anything else
