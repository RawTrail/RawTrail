# 0022. The laptop install profile: one owner, local passkey sign-in, long sessions

- Status: Accepted
- Date: 2026-11-26
- Deciders: getvictor

## Context

RawTrail was built as a fleet server: many Macs report to one server that a security team signs in to through single sign-on, with break-glass as a rarely used recovery path. The identity specs set that server's controls accordingly. Break-glass takes a password and a passkey, its sessions last 15 minutes idle and an hour in all, and it exists so an operator can recover from an identity provider outage, not to work in.

The laptop install (`docs/watch-your-agent.md`) serves a different person: a developer watching their own AI coding agent on their own Mac. It runs the same server, databases and console on that Mac (`docker-compose.laptop.yml`, installed by `scripts/laptop/rawtrail-laptop`). For that owner the server's controls were wrong in practice. They had no identity provider, so break-glass was their only sign-in. They typed a password on every visit, and were signed out after an hour, for a console that only they can reach.

The decisions that changed this landed one issue at a time (#1435, #1587, #1613, #1618, #1619) and live in the `laptop-*` OpenSpec changes, `docs/breakglass.md` and the threat model. They relax controls the identity specs set for a server, and no single document stated them together or said why that is acceptable.

## Decision

A laptop install is a deployment profile of the same server binary, selected by `EDR_PROFILE=laptop`, with its own sign-in posture:

- **Local only.** The stack publishes one port on `127.0.0.1` and the server terminates TLS with a certificate the installer made for `localhost`, which the agent pins. Nothing is reachable from the network.
- **Break-glass is the owner's sign-in.** The installer opens a one-time link that registers the passkey for `admin@rawtrail.local`. Single sign-on is not configured; it stays available behind the console's advanced settings, and nothing in the profile turns it off.
- **A passkey alone signs in.** The passkey-only routes are mounted on this profile only. They require the authenticator to verify the user (Touch ID or the device passcode) and check no password. A server install serves no such route and keeps requiring both.
- **Setup asks for a discoverable credential** (`residentKey: preferred`), so Touch ID works in a browser that remembers no email. Preferred, not required, so an authenticator that cannot store one still registers and signs in through its email.
- **Long sessions.** Break-glass sessions default to 7 days idle and 30 days absolute, against 15 minutes and an hour on a server. `EDR_BREAKGLASS_SESSION_IDLE_TIMEOUT` and `EDR_BREAKGLASS_SESSION_ABSOLUTE_TIMEOUT` still override them.
- **Unchanged from a server:** a destructive action (a host command, revoking an enrollment, editing rules or detection settings, resolving a critical alert) needs a sign-in within the reauth window (`EDR_REAUTH_WINDOW`, 30 minutes by default), and reauthenticating takes the password and the passkey. Each WebAuthn challenge is single-use. Every sign-in is audited, the passkey-only ones as `passkey_only`; as on a server, a failed audit write is logged as an error and does not block the sign-in.

## Consequences

**Easier.** The owner signs in with Touch ID and stays signed in for weeks, which is what a single-user developer tool has to do to be used at all. One binary, one schema and one release serve both installs, so a fix to detection or the agent reaches laptop owners without a second build.

**Harder, and these are the real costs.**

- **The profile is a security setting.** It used to choose the console's defaults and nothing else. It now decides whether a password is checked and how long a recovery-grade session lives, so a change to what reads `EDR_PROFILE` is a change to authentication and gets reviewed as one.
- **Nothing stops a server from claiming to be a laptop.** The server cannot tell from inside its container whether its port is published on loopback, so `EDR_PROFILE=laptop` on a networked server gives that server laptop sign-in. The guard is the compose file and the installer, not the binary, and `docs/install-server.md` says not to set it on a server reachable from the network.
- **A stolen session cookie lasts longer.** On a laptop it is good for up to 30 days, not an hour. The cookie is `HttpOnly`, `Secure` and `SameSite=Lax` and the console is reachable only from the Mac, so stealing it takes code running as the owner, which can already read what the console shows. The longer window is accepted for that reason, and is the first thing to revisit if the laptop install ever listens beyond loopback.
- **A passkey sign-in is not proof of who is at the keyboard.** Anyone who knows the Mac's passcode passes user verification, and a synced passkey works from the owner's other devices. The threat model records this. A sign-in starts the reauth window, so for as long as `EDR_REAUTH_WINDOW` allows (30 minutes by default) a passkey-only session runs destructive actions on the passkey alone; after that, the reauth prompt asks for the password as well. The password is therefore a limit on how long a passkey-only session acts unprompted, not a gate on every destructive action.

## Alternatives considered

**A separate single-user build.** A laptop binary could compile out single sign-on, fleet settings and multi-user administration entirely, which is attractive because no setting could then weaken a server. Rejected because it doubles the release, signing and test matrix, and the two installs would drift: the laptop install exists to show the fleet product's detection on one Mac. A profile with a short, reviewed list of what it changes keeps one build.

**Single sign-on against a local identity provider.** Shipping an identity provider in the laptop stack would keep break-glass a recovery path, as on a server. Rejected because it adds a service, its secrets and its upgrades to a stack meant to cost about 450 MB idle, and the owner would still authenticate to it with a password or a passkey. It moves the same decision into a component RawTrail does not own.

**Keeping the password factor and only lengthening sessions.** The smallest relaxation, and the first one shipped (PR #1615). Rejected as the end state because the owner still typed a password on every new browser and after every 30 days, and a platform passkey that verified its user is already two factors: something the owner has (the Mac's Secure Enclave key) and something they know or are (the passcode or Touch ID). The password on top added friction more than it added a factor.

**Requiring a discoverable credential.** `residentKey: required` would make usernameless Touch ID always work. Rejected because it refuses a security key that cannot store one, leaving its owner no way to set up at all.

## References

- Issues #1435 (laptop install), #1587 (laptop sign-in page), #1613 (long sessions and Touch ID sign-in), #1618 (single-use challenges), #1619 (sign-in with no email).
- PRs #1615 (session lifetimes), #1617 (Touch ID sign-in), #1622 (single-use challenges), #1623 (discoverable passkey).
- OpenSpec changes `laptop-session-lifetime`, `laptop-touch-id-signin`, `laptop-passkey-login`, `laptop-discoverable-passkey`, `breakglass-single-use-challenges`.
- `docs/watch-your-agent.md`, `docs/breakglass.md` ("On a laptop install"), `docs/threat-model.md` ("Sign-in without the password factor on a laptop install").
- ADR-0019 (TLS 1.3 only), whose floor the laptop server's own TLS keeps.
