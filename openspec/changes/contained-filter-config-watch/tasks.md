## 1. Host app

- [x] 1.1 Add the `filter-status` subcommand, printing `enabled` or `disabled` under the preferences watchdog.

## 2. Agent

- [x] 2.1 Read the subcommand's answer through the self-heal remediator, after the same host-app identity check.
- [x] 2.2 Check the configuration every 30 seconds while contained, and read a switched-off configuration as the content filter stopped.
- [x] 2.3 Serialize the check with the extension's reports, so health, the self-heal and the transition record see one sequence.

## 3. Verification

- [x] 3.1 Unit tests, mutation-tested.
- [x] 3.2 Reproduce the disable-filter loop on edr-dev with this agent and host app.
