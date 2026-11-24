## Why

A root user on a contained Mac left it uncontained while host health read healthy (#1540). A content filter whose NetworkExtension configuration is switched off never starts, so the network extension never reports it, neither running nor stopped. Health grades the report on what it names, so with the DNS proxy running it read healthy. The self-heal acts only on a provider reported stopped, and the tamper record sees no change, so nothing repaired the filter and nothing alerted. On edr-qa, after a reboot with the configuration off, containment failed with "content filter is not running" while health reported healthy with no `content_filter` component at all.

## What changes

- While the host is contained, the agent treats a decoded provider report that does not name the content filter as the filter stopped. Health then shows it unhealthy, the self-heal re-enables it, and a filter that was running and then went missing is recorded as a stop, which the server's tamper rule alerts on.
- A report that did not decode is left alone, since it arrives empty. An uncontained host's report is read as the extension sent it.
- The second defect in #1540 is separate and follows in another change: the restarted filter refusing the re-sent containment state as "not newer" while letting flows through.

## Impact

- `agent/cmd/rawtrail-agent/main.go` (`requiredProviders`, `observeProviderStatus`), `agent/cmd/rawtrail-agent/sensors_notwindows.go`.
