## 1. Implementation

- [x] 1.1 Return 403 `session_required` from `GET /api/session` when the context carries an actor but no session.
- [x] 1.2 Test the bearer path through the production `APIAuth` middleware, and the missing-actor path still returning 500.
- [x] 1.3 Document the 403 in the OpenAPI spec.
