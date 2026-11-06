## Why

Running RawTrail on your own Mac, to watch your own AI agents, takes a public domain and ports 80 and 443 for the quickstart, or the demo stack with its checked-in secrets and seeded data. Installing the agent means hand-writing a root-owned config with the server URL, enroll secret and certificate fingerprint, and the first login means finding a one-hour setup link in the logs (issue #1435).

## What changes

- `docker-compose.laptop.yml`: the server published on 127.0.0.1 only, terminating TLS itself with a certificate for localhost; ClickHouse capped near 1 GB; MySQL without its performance schema; 7-day retention; no telemetry export.
- `scripts/laptop/rawtrail-laptop install`: checks a Docker engine is running, generates secrets and a trusted certificate, starts the stack, writes `/etc/rawtrail.conf` pinned to the certificate, installs the notarized agent package, and opens the admin setup link.
- `rawtrail-laptop uninstall`: removes the agent it installed, the stack and its data, and the certificate's trust. An agent reporting to another server is left alone unless the user asks to repoint it, and its config is restored on uninstall.
- `rawtrail-laptop login` makes a fresh setup link; `status` shows the stack and the agent's server.

## Impact

- New files only: the compose profile, the ClickHouse config it mounts, and the installer. No server or agent code changes.
- test/arch checks the profile's exposure and sizing, and runs the installer against stubbed system tools.
