# Demo stack

The demo stack runs the RawTrail server, UI and detection pipeline in Docker, loaded with real telemetry captured on Macs and scrubbed. It needs no Apple Silicon Mac, MDM or Apple entitlement. It shows recorded data, not live capture: to watch your own agent, install on your Mac ([watch-your-agent.md](watch-your-agent.md)).

## Start it

```sh
docker compose -f docker-compose.demo.yml up
```

Open <https://localhost:8088/ui/>, accept the self-signed certificate warning, and sign in through the bundled IdP with the demo account `demo@rawtrail.local` / `demo`. It signs in as an **admin**: user invitations, policy authoring, and every host and alert action, so you can drive the full operator surface.

To build the demo images from source instead of pulling the release tag:

```sh
docker compose -f docker-compose.demo.yml -f docker-compose.demo.build.yml up --build
```

## What's in it

- **Agent sessions:** four Claude Code sessions captured on a Mac:
  - An ordinary coding session over three prompts.
  - A session in bypassPermissions mode that runs a subagent.
  - Two sessions the credential guard stopped: an honest mistake reading AWS credentials, and a prompt injection in a poisoned repository whose install hook reads a deploy key.
- **Hosts:** two macOS hosts, an engineer laptop and a CI build server, each with a deep process graph and correlated network and DNS activity.
- **Detections:** woven into that activity, five ATT&CK techniques that fire alerts:
  - A credential keychain dump.
  - A suspicious process phoning home, correlated across exec, DNS and the outbound connection.
  - Sudoers tampering.
  - Launchd persistence.
  - An application-control block.

The seeder posts every event through the same ingestion API an agent uses, so each alert is raised by the real detection pipeline, not inserted into the database.

## Notes

- **Super admin.** The demo account is an `admin` with SSO login. For a `super_admin` break-glass account, the server prints a one-shot `BREAK-GLASS ADMIN SETUP` redemption URL for `admin@rawtrail.local` on each boot until it is redeemed. It is valid for one hour; open it and register a WebAuthn passkey. Retrieve it with `docker compose -f docker-compose.demo.yml logs server | grep -A4 BREAK-GLASS`.
- **No live capture.** The on-device half (system extension, network extension, agent) needs an Apple-granted Endpoint Security entitlement and Apple Silicon, so it cannot run in Docker.
- **Response actions** enqueue but never complete, because no live agent is connected.
- **Evaluation only.** The stack uses an empty MySQL password, a self-signed certificate and checked-in dev secrets. Do not expose it to the internet.
- **Fresh data.** An existing demo volume keeps the data it was first seeded with. Run `docker compose -f docker-compose.demo.yml down -v` to start over with the current captures.
