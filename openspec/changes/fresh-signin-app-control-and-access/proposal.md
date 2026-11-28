## Why

The server asked for a fresh sign-in before host commands, enrollment revocation, dismissing a critical alert, and the deployment-wide writes to reachable addresses, rule content and detection tuning (#1575). Application control writes were not gated.

An allow rule, or a deleted block rule, lifts a block on every host its policy covers. That is the same class of change as detection tuning. The #1113 QA on v0.9.0-rc.8 found Add rule saving on a stale session with no prompt. The application control dialogs already keep unsaved work across an identity provider round trip (#1419), but with no server gate that code never ran.

## What changes

- Every application control write requires a session that signed in within the reauth window:
  - `application_control.rule_create`, `rule_update`, `rule_delete` and `rule_bulk_upsert`;
  - `application_control.policy_create`, `policy_update` and `policy_delete`.
- A stale session gets `403 reauth_required`. The console's existing reauth dialog and the dialogs' drafts take it from there.
- Reading application control is unchanged.
- Service accounts stay exempt, as for every other gated action.
- Access settings (sign-in, users, service accounts, webhooks) are the second part of #1575, in a later change to this same delta.

## Impact

- `server/identity/internal/authz/policy/edr.rego`. No API shape, schema or UI code change.
- Operator docs that list the gated actions: `docs/install-server.md`, `docs/okta-setup.md`, `docs/threat-model.md`.
