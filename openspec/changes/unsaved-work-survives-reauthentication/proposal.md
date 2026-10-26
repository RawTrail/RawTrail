## Why

An OIDC reauthentication leaves the page for the identity provider, so everything the page held in memory is lost (#1113). An operator who edits the reachable destinations with a stale session, or writes a rule, is redirected, comes back to the page, and finds the stored version rather than the draft. Nothing says the work was dropped. The dialog's own copy tells them to "re-run the action", which is a fair ask for a click and a poor one where the action carries typed work.

## What changes

The standard pattern for step-up authentication: keep the operator's input in the browser across the redirect, put it back on return, and never replay the action.

- Before it leaves the page, the reauthentication dialog saves the unsaved work of every surface that opted in, in `sessionStorage`. It says that the work will be kept, and that signing in saves nothing and re-runs nothing.
- On return the page puts the work back, with a notice that says so. The operator reviews it and saves again. The mutation that needed the sign-in is deliberately not replayed: an action gated on fresh authentication deserves a second look after the round trip.
- If the work could not be kept (storage refused it), the dialog says it will be lost and needs a second click to continue.
- Saved work is restored only on the page it came from, only under the surface's own key (one rule's work is not put into another's editor), only to the operator who wrote it (a sign-in that completes as someone else gets nothing), only if it is of the surface's own shape, and only within 15 minutes. A restored edit of the reachable destinations is saved against the version it was edited against, so a change someone else made meanwhile is refused as out of date, not overwritten.
- Adopted by the reachable destinations editor and the rule editor, the two surfaces #1113 names. The break-glass flow does not leave the page and is untouched.

## Impact

- `ui/src/reauthDraft.ts`, `ui/src/hooks/useReauthDraft.ts`, `ui/src/components/ReauthModal.tsx`, `ReauthDraftNotice`, `ReachableAddresses.tsx`, `RuleEditor.tsx`.
- Not in this change: the watched paths editor, the exclusion form and the application control dialogs, which need the same adoption (their dialogs also need reopening on return).
