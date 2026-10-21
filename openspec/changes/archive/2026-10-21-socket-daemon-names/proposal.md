# Name the daemon behind a socket an agent session connected to

## Why

A session's Unix-socket connects (issue #1355) name a path: `/private/tmp/com.apple.launchd.iIlTeYzViU/Listeners` says "ssh-agent" only to someone who knows launchd's layout. Naming the daemon makes the hand-off readable at a glance.

## What changes

- The Activity view's Files list names the daemon beside a `connect` row whose socket path is a well-known one: Docker (and Colima, OrbStack, Rancher Desktop), Podman, ssh-agent, 1Password's SSH agent, PostgreSQL, MySQL, Redis, and mDNSResponder for DNS.
- Process detail shows the same for a command's subtree, so the daemon a command talked to is named under it.

## Impact

UI only: a table of path patterns. A socket no entry matches is shown by its path alone.
