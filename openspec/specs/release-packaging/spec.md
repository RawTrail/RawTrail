# Release Packaging Specification

## Purpose

The release pipeline is the bridge between a tagged commit and a deployable artifact a customer can install. For a macOS endpoint product the packaging step is the difference between an experiment and a shippable build: an unsigned, un-notarized package will not install on a stock SIP-enabled Mac, and a signed package without the right entitlements installs but fails to load the system extension. This specification fixes the externally-visible invariants of that pipeline so that release engineers, auditors, and operators integrating the artifact through MDM channels (Fleet, Jamf, Kandji, Intune, mosyle) have a stable contract to verify.

The implementation chooses specific tools (xcodebuild, pkgbuild, productbuild, codesign, notarytool, GitHub Actions); this specification declares the externally observable outcomes those tools must produce, so the pipeline can change tools without changing the contract.

## Requirements

### Requirement: Dry-run build on any macOS runner

The release pipeline SHALL provide a dry-run build path that exercises the full pkgbuild and productbuild script flow without requiring Developer ID certificates, an Apple notary submission, or any release-only secret. The dry-run MUST succeed on a stock GitHub-hosted macOS runner so that pull requests touching the packaging scaffolding catch script regressions before release.

#### Scenario: Pull request runs the dry-run

- **GIVEN** a pull request that modifies any packaging script
- **WHEN** the dry-run workflow runs on a hosted macOS runner with no signing secrets present
- **THEN** the workflow completes without error
- **AND** the resulting `dist/` directory contains an unsigned `.pkg` whose component layout matches the production layout (agent component, app component, support component)

#### Scenario: Dry-run does not contact Apple

- **GIVEN** the dry-run workflow runs
- **WHEN** the build script reaches what would be the notarization or signing step in production
- **THEN** the script skips real signing and notarization and exits cleanly without attempting any network call to Apple

### Requirement: Real release build is gated to release tag refs

The release pipeline SHALL gate access to Developer ID Application and Developer ID Installer certificates, plus notarytool credentials, behind a protected GitHub Environment that is restricted to release tag refs. A workflow run from any other ref MUST NOT receive these secrets and MUST NOT produce a signed artifact.

#### Scenario: Tag push triggers a real build

- **GIVEN** a release tag matching the documented tag pattern is pushed
- **WHEN** the release workflow runs for that ref
- **THEN** the workflow decrypts the signing certificates from the protected environment and produces a signed, notarized package

#### Scenario: Manual trigger from a non-release ref does not sign

- **GIVEN** the release workflow is dispatched manually from a topic branch
- **WHEN** the workflow runs
- **THEN** the workflow runs in dry-run mode and does not access the protected signing environment

### Requirement: Hardened-runtime code signing of executables

Every Mach-O binary inside the released package SHALL be signed with the hardened runtime enabled and a secure timestamp so that downstream XPC peer validation, the macOS notary, and Gatekeeper accept the artifact at install and run time. The host app, the system extension bundle, and the network extension bundle (when present) MUST each be signed with their respective entitlements applied so that restricted entitlements take effect on SIP-enabled hosts.

#### Scenario: Notary accepts the package

- **GIVEN** a real release build of a tagged commit
- **WHEN** the package is submitted to the Apple notary
- **THEN** the notary accepts the submission

#### Scenario: System extension activates on a SIP-enabled host

- **GIVEN** a SIP-enabled macOS host that has installed the released package
- **WHEN** the host app requests activation of the system extension
- **THEN** activation succeeds (the per-bundle entitlements that the activation depends on are present in the signed bundle)

### Requirement: Notarization and stapling

A real release build SHALL submit the final installer package to the Apple notary, wait for acceptance, and staple the notarization ticket to the package so that Gatekeeper can validate the package offline at install time. A package that has not been stapled MUST NOT be published.

#### Scenario: Released package is stapled

- **GIVEN** a notarized installer package
- **WHEN** the build script completes
- **THEN** the package carries a stapled notarization ticket
- **AND** offline Gatekeeper validation of the package succeeds

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

### Requirement: Mobile configuration profiles ship alongside the package

The release pipeline SHALL produce three `.mobileconfig` profiles that operators upload to their MDM alongside the package: one that pre-approves the system extension so end users do not see the load-time approval prompt, one that grants the agent the TCC Full Disk Access entitlement it needs to read system telemetry, and one that marks the product's background items as managed so a console user cannot turn off the agent daemon or the activation LaunchAgent. All three profiles MUST be rendered with the project's team id substituted into the template, and MUST ship unsigned (plain XML, no CMS wrapper). The payloads are MDM-only, every supported MDM channel (Fleet, Jamf, Kandji, Intune, mosyle) signs profiles itself at delivery time, and Fleet rejects a pre-signed upload; download authenticity is provided by the cosign signature attached to each released artifact, not by a CMS signature on the profile.

The background items profile SHALL identify the items by the team identifier rather than by label or bundle identifier, so every background item the team signs is managed, including one a later release adds. The render step SHALL refuse to produce a background items profile whose rule does not name the team identifier.

#### Scenario: Profiles are rendered unsigned

- **GIVEN** a release build (real or dry-run)
- **WHEN** the profile render step runs
- **THEN** the build produces `edr-system-extension.mobileconfig`, `edr-tcc-fda.mobileconfig` and `edr-login-items.mobileconfig` with the team id substituted
- **AND** each profile is plain XML with no CMS signature, accepted verbatim by an MDM that signs at delivery time

#### Scenario: The background items profile manages the team's items

- **GIVEN** a release build rendering its profiles
- **WHEN** the background items profile is rendered
- **THEN** it carries one managed login items rule, of type team identifier, naming the project's team id
- **AND** the render step fails if the rendered rule names another type or team

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

### Requirement: Release artifacts carry a verifiable Sigstore signature

From the first release that ships the bundle format (v0.2.0) onward, the release pipeline SHALL publish, alongside every signed blob artifact (the package, all three `.mobileconfig` profiles, the `SHA256SUMS` manifest, and both SBOMs), a single Sigstore bundle file (`<artifact>.sigstore.json`) that carries the signature, the ephemeral Fulcio signing certificate, and the transparency-log proof in one file. Each bundle MUST verify the artifact against the GitHub Actions workflow identity that produced it, using only non-deprecated cosign flags, so a verifier on a current cosign release sees no deprecation warnings. The pipeline MUST NOT require the legacy `<artifact>.sig` + `<artifact>.pem` pair for releases that ship bundles; releases predating the bundle format keep their existing `.sig`/`.pem` files and the legacy verify command remains valid for them.

#### Scenario: Each released artifact verifies against its published bundle

- **GIVEN** a release tag whose pipeline signs every blob artifact with `cosign sign-blob --bundle`
- **WHEN** a verifier runs `cosign verify-blob --bundle <artifact>.sigstore.json --certificate-identity-regexp <release-workflow-identity> --certificate-oidc-issuer https://token.actions.githubusercontent.com <artifact>` on a current cosign release
- **THEN** verification reports "Verified OK"
- **AND** no deprecated-flag warning is printed

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

### Requirement: Packaged bundles carry the entitlements they need to activate

The build SHALL sign the host app and both system extensions with their entitlements in EVERY build mode, and SHALL fail if any of the three is missing the entitlement that makes it usable.

This is the only place those entitlements are applied. The Xcode project sets none, so every bundle it produces carries `get-task-allow` alone: an app without `com.apple.developer.system-extension.install` cannot submit an activation request, and an extension that declares no category is rejected by the operating system as not belonging to any extension category. Both failures appear only when somebody installs the package and watches activation fail, and the platform reports the reason in a form that is redacted by default, so the build MUST assert the entitlements rather than assume the signing step ran.

Restricted entitlements additionally require an embedded provisioning profile to be honoured on a host with System Integrity Protection enabled. Profiles are issued per signing identity, so a package built without release credentials cannot carry them, and such a package is usable only where those entitlements are not enforced.

#### Scenario: Every build mode applies the entitlements

- **GIVEN** a package built without release signing credentials
- **WHEN** the build completes
- **THEN** the host app is signed with the system-extension install entitlement
- **AND** each system extension is signed with the entitlement that declares its category

#### Scenario: A bundle missing its entitlement fails the build

- **GIVEN** a build in which one of the three bundles is signed without its entitlement
- **WHEN** the build reaches the entitlement check
- **THEN** the build fails and names the bundle and the missing entitlement

### Requirement: A package built for testing replaces what is already installed

A package built without release signing credentials SHALL replace the installed application regardless of the version already present.

Such a package is a test artifact for a development host, and it carries whatever version the local build produced rather than a release version. Under the version-checking the released package relies on, installing one over an existing install skips the application while still replacing the agent, and the installer reports success: the host is left running a new agent against the previously installed application and extensions. A mixed install that reports success is worse than a refused one, because nothing surfaces it.

The released package SHALL keep version-checking, so a genuine upgrade replaces the application only when its version moves.

#### Scenario: A test package installs over an existing install

- **GIVEN** a host with the application already installed
- **WHEN** a package built without release signing credentials is installed, whatever version it carries
- **THEN** the installed application is replaced by the one in the package

### Requirement: The packaged agent carries the identifier the extension expects

The system extension authenticates its XPC peer partly by code-signing identifier, so the agent shipped in the package MUST carry the identifier the extension is built to accept. The identifier SHALL be set explicitly at signing time rather than inherited from the signing tool's default, because that default varies with the signing mode: an ad-hoc signature of a bare executable derives a name that includes a content hash, which the extension does not accept.

This applies to the dry-run path as well as the release path. A package whose agent cannot establish its extension session installs successfully and then produces no telemetry, with the failure visible only as a health condition, so the dry-run path being usable is part of what makes it a meaningful rehearsal.

#### Scenario: The packaged agent is accepted by the extension

- **GIVEN** a package built by the dry-run path, without release signing material
- **WHEN** it is installed on a host whose extension is built for development
- **THEN** the agent establishes its session with the extension
- **AND** the extension component does not report never-connected

#### Scenario: The identifier does not vary with signing mode

- **WHEN** the agent is signed by either the dry-run or the release path
- **THEN** the resulting binary carries the same code-signing identifier

### Requirement: The installer refuses a macOS the bundles cannot run on

The installer SHALL refuse to install on a Mac whose macOS version is older than the deployment target the app and both system extensions are built for, and SHALL name the required macOS version in the refusal. The installer minimum and the deployment target MUST be the same version, so no Mac can install a package it cannot run. The supported floor is macOS 26.0 on Apple Silicon.

#### Scenario: The installer minimum matches the deployment target

- **GIVEN** the app and both extensions are built for one macOS deployment target
- **WHEN** the release package is assembled
- **THEN** the installer's minimum macOS version equals that deployment target
- **AND** the installer's scripted version check refuses every macOS major version below it

#### Scenario: An older Mac is refused with the requirement named

- **GIVEN** a Mac running macOS 15
- **WHEN** an operator opens the release package
- **THEN** the installer refuses before installing anything
- **AND** the message says RawTrail requires macOS 26 (Tahoe) or later
