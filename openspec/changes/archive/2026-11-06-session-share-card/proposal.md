## Why

People who watch their AI agents want to show what an agent did, and a screenshot of the console shows usernames, hostnames, paths and command lines that name them and their machine (issue #1430). A shared image has to be safe by construction, not by the person remembering to crop.

## What changes

- A share card model: what a session shared as an image says, built from the Summary's counts, the programs its commands ran (names only, never arguments), and the public domains it talked to. It is the only input the image renderer gets.
- Redaction applied to every string on it: usernames, hostnames, IP addresses and user@host pairs are removed, and home folders become `~`. Domains that are addresses never appear; internal ones (an internal suffix, or the domain the host is named under) appear only if the person chooses.
- The person's choices: a prompt shown as the title, edited first; the project's name; internal domains; what the credential guard stopped. Off by default except the guard.
- A suggested caption and alt text, built from the card alone.
- A later change in this delta adds the Share action, the image renderer and the dialog.

## Impact

- UI only: `shareRedact.ts` and `shareCard.ts` under `ui/src/components/Sessions/`, with literal and property tests.
