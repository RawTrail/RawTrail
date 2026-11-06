## Why

The v0.9.0-rc.1 release run failed twice at the same step: the Apple notary accepted the package, then `stapler staple` failed with exit 68 because the runner lost its connection to Apple's ticket service. The build tried the fetch once, so a transient network failure failed the whole release run.

## What changes

- `packaging/pkg/build.sh` retries `stapler staple` up to five times, with waits of 30, 60, 90 and 120 seconds, verbose so a lasting failure shows its cause. The package is still validated after stapling, and is never published unstapled.
- `packaging/pkg/build.test.sh` runs the staple block with stubbed Apple tools, and the packaging workflow runs it.

## Impact

- `packaging/pkg/build.sh`, `packaging/pkg/build.test.sh`, `.github/workflows/pkg-dryrun.yml`.
