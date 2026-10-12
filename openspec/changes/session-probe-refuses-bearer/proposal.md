## Why

`GET /api/session` returned `500 session_misconfigured` when called with a valid service-account bearer token (#1268). The operator API accepts a bearer token and pins an actor but no browser session, and the handler read the missing session as a wiring fault. The caller sent a credential this route does not take: a client error, and a 500 also pages whoever alerts on server errors.

## What Changes

- A service-account bearer token on `GET /api/session` gets `403 {"error":"session_required"}`. The token is valid, so 401 (present credentials again) would mislead.
- A request that reaches the handler with neither a session nor an actor is still a wiring fault and stays a 500.
- `DELETE /api/session` is unchanged: it is public, reads only the cookie, and returns 204 for any caller.

## Impact

- `server/identity/internal/login/handler.go`, the OpenAPI spec for `GET /api/session`.
