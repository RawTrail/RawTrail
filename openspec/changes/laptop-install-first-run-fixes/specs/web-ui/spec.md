## ADDED Requirements

### Requirement: A laptop opens its setup checklist after the first admin is set up

On a server whose profile is `laptop`, the console SHALL open the first-run checklist right after the first admin's passkey is registered, for an operator who can read hosts. Any later visit to the console's root SHALL open Agent sessions as before, and a `server` profile SHALL keep the alert queue first after setup too.

#### Scenario: The first sign-in lands on the checklist

- **GIVEN** a laptop install whose first admin has just registered their passkey
- **WHEN** the setup page hands them to the console
- **THEN** the console shows **Set up this Mac**, and a later visit to the root shows Agent sessions

### Requirement: The first-run checklist asks for a restart an upgrade left pending

When the Mac's agent reports that its network extension needs a restart to finish an upgrade, the checklist SHALL show network filtering as a step that needs the person, telling them to restart the Mac, instead of waiting for an approval macOS will not ask for until then.

#### Scenario: A pending restart is asked for

- **GIVEN** a Mac whose agent reports the network extension `reboot_required`
- **WHEN** the person opens the checklist
- **THEN** network filtering is shown red, saying to restart this Mac because an upgrade on some Macs needs a restart before network events resume, and it turns done once filtering is capturing
