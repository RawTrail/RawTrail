## 1. Server

- [x] 1.1 A revoke bumps `token_epoch` along with `revoked_at`, in one statement
- [x] 1.2 The revoke commits its audit entry to `endpoint_audit_outbox` in the same transaction, and `fleet-edr-server` runs the endpoint audit sweep
- [x] 1.3 Remove the rotate route, `RotateToken`, `BumpTokenEpoch`, and the `enrollment.rotate_token` authorization and audit actions
- [x] 1.4 Tests: a token issued before a revoke is refused by verify and refresh after re-enrollment; a refused audit step rolls the revoke back; the delivered audit row carries principal, address, host, actor and reason
- [x] 1.5 Live: revoke a host on the dev server, let it re-enroll, and confirm the pre-revoke token is refused and the audit row is recorded

## 2. Docs

- [x] 2.1 Operator docs say a revoke forces re-enrollment and does not block the host
