## Why

Revoking a host did not evict a stolen host token (issue #1225). A revoke marked the enrollment revoked but left the host's token epoch unchanged. The real agent re-enrolls with the fleet-wide enroll secret within minutes of being refused, and re-enrollment clears the revoked flag while preserving the epoch, so a token copied off the host before the revoke became valid again and the refresh route kept renewing it.

A separate route, `POST /api/enrollments/{host_id}/rotate`, bumped the epoch but had no client: no UI, agent, script or operator doc used it. Two verbs that nearly did the same thing, one of them weaker than its name, is the wrong shape for an incident-response control.

The revoke also wrote its audit row after its change committed, so a failed audit write or a crash between the two left a revoked host with nothing recording who revoked it (ADR-0011).

## What changes

- A revoke bumps the host's token epoch in the same statement that marks it revoked. A token issued before the revoke stays rejected after the agent re-enrolls, and cannot be refreshed.
- The revoke's audit entry commits in the revoke's own transaction, into a new endpoint audit outbox, and the shared audit drain delivers it. The row names the authenticated principal, the request's address, the host, and the operator-supplied actor and reason.
- `POST /api/enrollments/{host_id}/rotate`, its service method, the `enrollment.rotate_token` authorization action and its audit action are removed.

## Not changed

A revoke still does not keep a host out: an agent holding the enroll secret re-enrolls. Blocking re-enrollment until an admin re-admits the host is issue #1265.
