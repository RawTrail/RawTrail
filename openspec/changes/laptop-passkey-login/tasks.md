## 1. Server

- [x] 1.1 `GET /api/auth/options` answers without a session with `{"profile": ...}`
- [x] 1.2 Document it in `docs/api/openapi.yaml`

## 2. Console

- [x] 2.1 Move the passkey form into `PasskeySignInForm`, shared by the break-glass page and the login page
- [x] 2.2 The login page reads the profile and, on a laptop, shows only the passkey form
- [x] 2.3 UI tests for both profiles, the wait for the profile, and a failed read
