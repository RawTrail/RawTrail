## Why

The credential guard covers `~/.aws/credentials`, which holds only long-lived access keys. Teams that sign in with IAM Identity Center (`aws sso login`) or assume roles keep their live AWS secrets in two caches the guard does not watch (#1597):

- `~/.aws/sso/cache/*.json` holds the SSO access token, which can mint credentials for every account and role in the person's portal.
- `~/.aws/cli/cache/*.json` holds temporary STS credentials.

An agent told to `cat ~/.aws/sso/cache/*.json` gets past the guard in both detect and protect mode. The cache file names are hashes, so neither a literal nor a prefix entry can name them.

## What Changes

- A `directory` match mode covers every file directly in a directory, and none in its subdirectories.
  - The server validates it like a prefix: it must lie below a directory in the home, with no trailing `/`.
  - The extension mutes it as the directory's path with a trailing `/` and decides only direct children.
  - The policy editor offers it as "Files directly in this directory".
- The built-in policy, on the server and in the extension, adds `.aws/sso/cache` and `.aws/cli/cache` as directory entries of `aws_credentials`, owned by the AWS CLI's team (`94KV3E626L`).
- `~/.aws/config` stays out of the default policy: it normally holds no secrets, and agents and SDKs read it constantly.

## Impact

- `server/rules/api/credentialguard.go`: the match mode, validation and defaults.
- `extension/edr/extension/CredentialGuard.swift`: `CredentialMatch`, muting, coverage and the built-in list.
- `ui/src/components/DetectionConfig/CredentialGuard.tsx`: the new option.
- `docs/api/openapi.yaml`, `docs/operations.md`, `docs/detection-rules.md`, and the `agent_credential_read` rule's description.
- An extension older than this skips a directory entry as an unknown match and counts it, as it does any credential it cannot guard. It keeps guarding the rest.
- A fleet whose policy an operator has already changed keeps that policy. The operator adds the caches from the policy editor.
