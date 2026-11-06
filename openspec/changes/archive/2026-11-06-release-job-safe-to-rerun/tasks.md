## 1. Workflow

- [x] 1.1 Create the release as a draft, reuse a draft on a re-run, and refuse a published release.
- [x] 1.2 Upload the Sigstore bundles with `--clobber`, and publish as the last step with `--latest` only for a general release.
- [x] 1.3 Retry the cosign install twice.

## 2. Tests

- [x] 2.1 `tools/releaseworkflow` runs the real step scripts against stubbed `gh` and `cosign`, and pins the step order and the retry shape.

## 3. QA

- [ ] 3.1 Cut the v0.9.0 RC and re-run the job after a forced failure (tracked in the v0.9.0 RC issue).
