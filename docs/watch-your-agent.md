# Watch your AI agent

Your coding agent keeps a log of what it says it did. RawTrail records what actually ran: every process its commands started, the files they changed, the hosts they reached, and any credential file they opened, seen from the kernel on your own Mac. You run the whole thing on your laptop, and nothing leaves it.

## What you see

Open **Agent sessions** in the console and pick a session:

- **Summary.** What the agent's log reports beside what the kernel saw run: tool calls against processes, files changed (inside and outside the project), the programs it ran most and the domains it talked to. The card can be copied or downloaded as an image to share; usernames, hostnames, addresses and command arguments never appear on it.
- **Timeline.** Each prompt you typed with the commands and tool calls that followed it, in order. Credentials in a prompt are replaced with a placeholder before the agent sends it.
- **Process tree.** Every process the session started, with its files, connections and DNS lookups.
- **Credential guard.** An alert when a process in the session opens an SSH key, cloud credentials or another credential file with anything but that file's own tool. In protect mode the read is refused.

RawTrail observes Claude Code sessions today.

## Install

You need an Apple Silicon Mac on macOS 26 or later and a running Docker engine (Docker Desktop, OrbStack or Colima, with about 3 GB of memory for it). From a clone of the repository:

```sh
scripts/laptop/rawtrail-laptop install
```

The installer:

1. Starts the server stack in Docker, reachable from this Mac only, on `https://localhost:8443`. It generates its secrets and a TLS certificate for `localhost` under `~/.rawtrail-laptop`.
2. Installs the notarized agent package and points it at that server, pinned to its certificate. It asks for your password for this step.
3. Opens the one-time link that registers your admin passkey (Touch ID). The link is valid for an hour; `scripts/laptop/rawtrail-laptop login` makes a new one.

macOS then asks you to allow RawTrail's system extensions in System Settings. The console's setup checklist shows what is still needed, such as Full Disk Access, and confirms when the agent is reporting. Start a Claude Code session and it appears under **Agent sessions**.

`scripts/laptop/rawtrail-laptop status` shows whether the stack and the agent are running. If the Mac already reports to another RawTrail server, `install` stops; `--replace-config` points it at the laptop server instead, and `uninstall` restores the previous config.

## What it costs

The stack keeps 7 days of events. MySQL is capped at 1,024 MB of memory and ClickHouse at 1,280 MB; the server itself has no cap. An idle stack uses about 450 MB in all.

| Measure                                   | Value            |
| ----------------------------------------- | ---------------- |
| Memory at idle                            | about 450 MB     |
| Memory over a working week of agent use   | not yet measured |
| Disk per day of agent use                 | not yet measured |
| Time from `install` to first session seen | not yet measured |

## Privacy

The server listens on `127.0.0.1` only and sends no telemetry. Prompts are stored on your Mac with credentials redacted. To keep prompts out entirely, turn prompt capture off in **Admin settings > Data collection**; commands and tool calls are still recorded. What the redaction catches is in [install-agent-manual.md](install-agent-manual.md).

## Uninstall

```sh
scripts/laptop/rawtrail-laptop uninstall
```

It removes the agent and its system extensions, the server stack and its data, and the trust in your login keychain it added for the certificate, if any. If macOS cannot remove the extensions yet, it says what to do and keeps enough in place for you to run it again.

## Going further

The same agent and server run a fleet. To monitor many Macs, deploy the server on its own host ([quickstart-vm.md](quickstart-vm.md)) and push the agent with your MDM ([mdm-deployment.md](mdm-deployment.md)).
