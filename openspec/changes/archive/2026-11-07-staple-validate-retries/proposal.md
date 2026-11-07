## Why

The v0.9.0-rc.2 release run stapled the ticket, then failed on `stapler validate`, which also asks Apple's ticket service and lost its connection the same way. The retry #1533 added covered stapling only.

## What changes

- `packaging/pkg/build.sh` retries `stapler staple` and `stapler validate` each on its own through one helper, up to five tries with a growing wait. A failed validation is retried without stapling again.
- `packaging/pkg/build.test.sh` covers a failed validation retried and one that gives up, and its stub refuses any call that is not the expected stapler command on the package.

## Impact

- `packaging/pkg/build.sh`, `packaging/pkg/build.test.sh`.
