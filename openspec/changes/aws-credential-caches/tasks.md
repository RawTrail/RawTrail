## 1. Server

- [x] 1.1 Accept `directory` as a credential match, below a directory in the home
- [x] 1.2 Add `.aws/sso/cache` and `.aws/cli/cache` to `DefaultCredentialGuard`

## 2. Extension

- [x] 2.1 `CredentialMatch` with `directory`: muted as the directory with a trailing `/`, covering only its direct children
- [x] 2.2 Add both caches to `CredentialGuard.builtIn`, in sync with the server

## 3. Console and docs

- [x] 3.1 Offer "Files directly in this directory" in the policy editor
- [x] 3.2 OpenAPI, operations and detection-rule docs list the new files and match

## 4. QA

- [ ] 4.1 On a Mac with SIP on: `aws sso login` and an SSO-profile `aws s3 ls` work in protect mode, and an agent session's `cat` of a cache file is refused and alerted
