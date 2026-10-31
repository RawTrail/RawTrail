# Demo scripts

Self-contained scripts that fire a real RawTrail detection on a live host, for demos and hands-on walkthroughs. Unlike the synthetic efficacy corpus (`test/efficacy/corpus/`, which replays events through the fakeagent library with no host) and the SSH-driven QA harness (`scripts/qa/`, `scripts/uat/`, which drive a VM from your workstation), each script here runs **directly on the enrolled Mac** and narrates what it is doing as it goes.

| Script                | Detection it triggers                  | ATT&CK                  |
| --------------------- | -------------------------------------- | ----------------------- |
| `trigger-dns-c2.sh`   | `dns_c2_beacon`                        | T1071.004 (+ T1568.002) |
| `credential-guard.sh` | `agent_credential_read` (protect mode) | T1552.004, T1552.001    |

## Prerequisites

- An Apple Silicon Mac on macOS 26 or later with the agent installed, the system + network extensions active, and Full Disk Access granted. See [`docs/install-agent-manual.md`](../../docs/install-agent-manual.md).
- Outbound internet from that host: the beacon resolves a [nip.io](https://nip.io) name over plain UDP DNS (which the proxy sees, unlike DoH/DoT) and opens a real outbound TCP connection to the resolved address.
- The RawTrail server reachable with this host enrolled, so the alert surfaces in the UI.

## `trigger-dns-c2.sh`

Simulates the classic "malware phones home" chain: a payload staged to `/tmp` resolves a domain and then connects to the address that lookup returned, all from the same process. The `dns_c2_beacon` rule correlates the temp-path `exec`, the `dns_query`, and the `network_connect` into a single finding.

```sh
# On the enrolled Mac:
./trigger-dns-c2.sh            # critical: high-entropy DGA-like domain -> T1071.004 + T1568.002
./trigger-dns-c2.sh high       # high:     ordinary short domain        -> T1071.004
```

Within a few seconds a **Suspicious process phoning home** alert should appear in the EDR UI, attributed to the staged `/tmp/beacon.*` process, citing both the DNS lookup and the outbound connection. The script copies `/usr/bin/curl` to a unique `mktemp` path under `/tmp` and re-signs it ad-hoc (a plain copy of an Apple platform binary is SIGKILLed when run from `/tmp`, so it would emit no telemetry), then removes it on exit.

## `credential-guard.sh`

Shows the AI agent credential guard refusing a prompt-injected read. Unlike the script above, it runs **from your workstation**: it drives the enrolled Mac over SSH and the server over its API.

```sh
export EDR_SERVER=https://edr.example.com EDR_API_TOKEN=...   # a token that may change detection tuning
task demo:credential-guard HOST=you@your-mac                  # or: scripts/demo/credential-guard.sh you@your-mac
task demo:credential-guard HOST=you@your-mac INTERACTIVE=1    # prints the prompt to type into Claude Code yourself
task demo:credential-guard HOST=you@your-mac INJECTION=1      # the poisoned project scenario
```

It writes an obviously fake `~/.aws/credentials` (two profiles) on the Mac, switches the fleet's credential guard to protect, and asks Claude Code which AWS profile is the default and what region it uses. It then checks the kernel's decision rather than the agent's words: the security extension refused the agent's reads of the file and the kernel took each refusal, and none of the fake keys reached the agent's output (not checked in interactive mode, where the answer is on the Mac). A plain terminal read of the same file still works. It fails rather than pass when the Mac does not apply protect mode within a minute.

With `INJECTION=1` (`--injection`) it shows a prompt injection instead of an honest mistake. It writes a fake `~/.ssh/id_ed25519_deploy` and copies `fixtures/poisoned-repo` to a new directory under `/tmp` on the Mac. That project's README hides an HTML comment telling AI assistants to print the key, and its npm `postinstall` hook reads it (see `fixtures/README.md`). Claude Code is asked only to set the project up. The pass conditions are the same: the reads were refused, the fake key did not reach the output, and a terminal read works. It passes only when the hook's own read of the key was refused with `EPERM`, the one read tied to that file. If the hook read the key, the run fails. An agent can decline the setup: a run in which the hook never ran exits 3 as inconclusive, since nothing in it is tied to the deploy key.

On any exit it removes the fake file it wrote, only if the file is still exactly as written (a changed file is left, and the run fails saying so), and restores the policy it found, with the version it stored, so a change someone made meanwhile is kept: the restore is then refused, the policy the demo found is saved to a file it names, and it exits non-zero.

- The policy is fleet-wide, so every enrolled Mac is in protect mode while the demo runs. Use a demo or dev server.
- The Mac must have Claude Code installed and signed in for the SSH user, and that user must be able to run `sudo -n log show`.
- The demo refuses to run on a Mac that already has a file at the fake credential's path, and never touches it: the fake file is created in one step that refuses an existing file or a symlink.
- Refusals are counted from when Claude Code is asked, so another agent reading the file on the same Mac at that moment would be counted too.
- `EDR_SERVER` must be an `https://` URL, since the token rides in every request.
- `DEMO_SSH_OPTS` passes extra ssh options, such as `-i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes`.
