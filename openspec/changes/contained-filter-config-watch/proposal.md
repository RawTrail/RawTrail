## Why

Issue #1540, part (b). On a contained host, a root user running `edr disable-filter` in a tight loop leaves the host uncontained while health reads healthy. Reproduced on edr-dev with nesessionmanager's log: when a `disable-filter` lands within milliseconds of the self-heal's `enable-filter`, macOS tears the filter session down while the provider is still starting and never calls the provider's `stopFilter`. The provider goes on reporting itself running, no flows reach it, and the configuration stays switched off. Every later `disable-filter` is a no-op, so nothing changes until the configuration is saved again, which re-contained the host within seconds on edr-dev.

The provider's own report cannot see this, so the agent has to read the configuration itself.

## What changes

- The host app gains a read-only `filter-status` subcommand that prints whether the content filter's configuration is enabled.
- While the host is contained, the agent reads it every 30 seconds. A configuration that is switched off reads as the content filter stopped, whatever the provider reports, so health shows it unhealthy, the self-heal re-enables it, and its stop is recorded.

## Impact

- `extension/edr/edr` (host app), `agent/selfheal`, `agent/cmd/rawtrail-agent`.
- One host-app process every 30 seconds on a contained host only. An uncontained host runs nothing new.
