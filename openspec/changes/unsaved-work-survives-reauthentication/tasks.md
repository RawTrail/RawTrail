## 1. Console

- [x] 1.1 Draft storage with a time to live, a page check and a registry of surfaces, and a hook that registers a surface's unsaved work and hands back what was saved.
- [x] 1.2 The OIDC reauthentication dialog saves the work before it leaves, says what will happen, and refuses to leave silently when the work could not be kept.
- [x] 1.3 The reachable destinations editor and the rule editor adopt it and show a notice when work is restored.
- [x] 1.4 Tests for the storage rules, the hook, the dialog and both surfaces.

## 2. QA

- [ ] 2.1 Against a real OIDC flow (tracked in the v0.9.0 RC issue): edit the reachable destinations with a stale session, sign in again, and confirm the draft is back and nothing was saved.
