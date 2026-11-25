## 1. Server

- [x] 1.1 Migration: `breakglass_consumed_challenges` keyed on the challenge hash, with its expiry
- [x] 1.2 Record the challenge in `VerifyLogin` (password sign-in and reauth) and `FinishPasskeyLogin`, refusing a repeat
- [x] 1.3 Sweep expired records on the identity cleanup timer
- [x] 1.4 `sqlhelpers.IsDuplicateKeyErr`, shared
