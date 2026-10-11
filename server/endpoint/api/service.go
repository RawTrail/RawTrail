package api

import "context"

// Service is the endpoint bounded context's full business surface. The agent enroll endpoint, the host-token middleware, and the
// operator-facing listing/revoke endpoints all consume this. Other contexts that need endpoint functionality (today: cmd/main wires
// the host-token middleware into the agent route stack) call into it through this api.Service interface.
type Service interface {
	// Enroll handles the agent enroll flow: validates the enroll secret + hardware UUID, persists the enrollment row, and mints a
	// self-validating signed bearer token. sourceIP is the resolved client IP (the caller is responsible for any X-Forwarded-For
	// handling). Returns ErrInvalidSecret on bad secret and ErrInvalidHardwareUUID on malformed UUID; both are 401/400 mappings the
	// handler does.
	Enroll(ctx context.Context, req EnrollRequest, sourceIP string) (EnrollResponse, error)

	// VerifyToken resolves a presented bearer token to a host_id. Used by the HostToken middleware on every agent request. The token is
	// self-validating (local signature + expiry check); revocation is enforced via an in-memory snapshot, so this does no database
	// lookup. Returns ErrInvalidToken on any kind of mismatch (unknown, revoked, expired, malformed); the caller does not distinguish
	// those cases, which would be an oracle.
	VerifyToken(ctx context.Context, token string) (string, error)

	// RefreshToken issues a fresh signed token for the presented (already host-token-authenticated) token. The agent calls this before
	// its current token expires so a live host never lapses. It re-verifies the presented token and rejects it if the host is unknown or
	// revoked or the token's epoch is below the host's current epoch (closing the revocation-snapshot staleness window); the handler maps
	// that to 401, prompting the agent to re-enroll.
	RefreshToken(ctx context.Context, token string) (RefreshResponse, error)

	// List returns operator-visible enrollment rows.
	List(ctx context.Context) ([]Enrollment, error)

	// RecordStatus persists the latest agent health snapshot for hostID, last-writer-wins ordered by the snapshot's ReportedAtNs, and
	// computes + stores the server-side overall rollup. Called by the host-token-authed POST /api/status check-in (the host_id is the
	// authenticated identity the middleware pinned, not a body field). Returns ErrInvalidStatusReport when a component carries a status
	// outside the closed HealthStatus set; unknown component type/reason strings are accepted and stored verbatim.
	RecordStatus(ctx context.Context, hostID string, report StatusReport) error

	// Get returns a single enrollment row. Returns ErrNotFound when the
	// host_id has no enrollment.
	Get(ctx context.Context, hostID string) (*Enrollment, error)

	// Revoke ends every credential the host holds: it marks the enrollment revoked and bumps the host's token epoch, so a token issued
	// before the revoke stays rejected after the agent re-enrolls with the enroll secret. The audit entry commits with the revoke. A
	// repeat revoke preserves the original revoked_at, reason and actor. Returns ErrNotFound when the host_id has no enrollment.
	Revoke(ctx context.Context, req RevokeRequest) error

	// CountActive returns the count of non-revoked enrollments. Used
	// by metrics (the EnrolledHosts gauge in cmd/main).
	CountActive(ctx context.Context) (int, error)

	// ActiveHostIDs returns the non-revoked host_ids in stable order.
	// Used by the policy fan-out path.
	ActiveHostIDs(ctx context.Context) ([]string, error)

	// ActiveEnrollments returns each non-revoked host_id with the time it last enrolled, on the database clock, in stable order.
	ActiveEnrollments(ctx context.Context) ([]ActiveEnrollment, error)
}

// RevokeRequest is an operator's revoke of one host's enrollment. Reason and Actor are the operator-supplied attribution stored on the
// enrollment row and carried in the audit entry; RemoteAddr is the address the request came from, as the audit trail records it. The
// authenticated principal comes from the request context, where the authorization gate put it.
type RevokeRequest struct {
	HostID     string
	Reason     string
	Actor      string
	RemoteAddr string
}
