## 1. Agent

- [x] 1.1 `NEUpgradeDiagnostics`: the product's `systemextensionsctl list` and `launchctl print system` lines.
- [x] 1.2 The receiver loop's `UpgradeDiagnostics` hook, gathered with the reboot-required hint once per episode, wired on the network extension loop.

## 2. Tests

- [x] 2.1 `productLines` keeps only the product's lines, trimmed; the hint carries the diagnostics and gathers them once.

## 3. Verify

- [x] 3.1 Run `NEUpgradeDiagnostics` on a Mac with old extension versions pending removal and read what it records.
