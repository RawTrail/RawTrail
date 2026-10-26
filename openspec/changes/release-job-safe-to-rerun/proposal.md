## Why

The release job could not be re-run after a transient failure, and it could leave a public release half published (#1215). On 2026-10-05 `v0.7.0-rc.3`'s pkg job failed at `Install cosign` when GitHub returned a 500 during an Actions incident, after `Create GitHub Release` had already published seven assets. The Sigstore bundles, uploaded later in the same job, were never made, so the public prerelease carried artifacts `verify-release` rejects. A re-run could not repair it because `gh release create` fails when the release exists, and the only recoveries were deleting the public release or cutting a new RC number.

## What changes

- The release is created as a draft and published by the job's last step, after every artifact and bundle is uploaded and the provenance is attested. A failure leaves only a draft.
- A re-run finds the draft, uploads every artifact and bundle over it with `--clobber`, and carries on. A release that is already published is refused, not overwritten.
- The cosign install is attempted up to three times, each only when the one before failed.

## Impact

- `.github/workflows/release.yml`, the `macos-pkg` job.
- Not changed: the docker jobs' own cosign installs, which sign images already pushed and have no release to half publish.
