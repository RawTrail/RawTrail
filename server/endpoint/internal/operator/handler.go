// Operator-facing endpoint routes:
//
//	GET  /api/enrollments                          - list enrollments
//	POST /api/enrollments/{host_id}/revoke          - revoke an enrollment
//
// Both are session-gated by cmd/main's wiring (Session + CSRF middleware).
// The handler only takes an api.Service; cmd/main passes a Service that
// is bound to the same store backing the agent-facing enroll handler.

package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/rawtrail/rawtrail/server/attrkeys"
	"github.com/rawtrail/rawtrail/server/endpoint/api"
	"github.com/rawtrail/rawtrail/server/httpserver"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
)

// revokeBodyCap caps the JSON body size for POST /api/enrollments/{host_id}/revoke. The handler only reads two short string fields;
// matches the 64KiB cap the admin operator surfaces use and protects the operator surface from clients that stream a large body
// before any auth check fails.
const revokeBodyCap = 64 << 10

// Handler serves the operator-facing enrollment routes.
type Handler struct {
	svc    api.Service
	authz  identityapi.AuthZ
	logger *slog.Logger
}

// New builds an operator handler. Panics if svc or authz is nil. authz is the authorization chokepoint every privileged route gates
// on; a nil one would silently bypass the role matrix.
func New(svc api.Service, authz identityapi.AuthZ, logger *slog.Logger) *Handler {
	if svc == nil {
		panic("operator.New: api.Service must not be nil")
	}
	if authz == nil {
		panic("operator.New: authz must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{svc: svc, authz: authz, logger: logger}
}

// RegisterRoutes wires the operator routes on the given mux.
func (h *Handler) RegisterRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/enrollments", h.handleList)
	mux.HandleFunc("POST /api/enrollments/{host_id}/revoke", h.handleRevoke)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionEnrollmentRead, identityapi.Resource{Type: "enrollment"}) {
		return
	}
	rows, err := h.svc.List(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "list enrollments", "err", err)
		writeErr(ctx, h.logger, w, http.StatusInternalServerError, "internal")
		return
	}
	if rows == nil {
		rows = []api.Enrollment{}
	}
	writeJSON(ctx, h.logger, w, http.StatusOK, rows)
}

type revokeRequest struct {
	Reason string `json:"reason"`
	Actor  string `json:"actor"`
}

func (h *Handler) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hostID := r.PathValue("host_id")
	if hostID == "" {
		writeErr(ctx, h.logger, w, http.StatusBadRequest, "missing host_id")
		return
	}
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionEnrollmentRevoke, identityapi.Resource{Type: "enrollment", ID: hostID}) {
		return
	}

	var body revokeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, revokeBodyCap)).Decode(&body); err != nil {
		writeErr(ctx, h.logger, w, http.StatusBadRequest, "bad_body")
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	body.Actor = strings.TrimSpace(body.Actor)
	if body.Reason == "" || body.Actor == "" {
		writeErr(ctx, h.logger, w, http.StatusBadRequest, "reason and actor are required")
		return
	}

	err := h.svc.Revoke(ctx, api.RevokeRequest{HostID: hostID, Reason: body.Reason, Actor: body.Actor, RemoteAddr: httpserver.ClientIP(r)})
	switch {
	case errors.Is(err, api.ErrNotFound):
		writeErr(ctx, h.logger, w, http.StatusNotFound, "not_found")
		return
	case err != nil:
		h.logger.ErrorContext(ctx, "revoke enrollment", "err", err)
		writeErr(ctx, h.logger, w, http.StatusInternalServerError, "internal")
		return
	}

	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String(attrkeys.AdminAction, "revoke"),
		attribute.String(attrkeys.AdminActor, body.Actor),
		attribute.String(attrkeys.HostID, hostID),
	)
	h.logger.WarnContext(ctx, "admin action",
		attrkeys.AdminAction, "revoke",
		attrkeys.AdminActor, body.Actor,
		attrkeys.AdminReason, body.Reason,
		attrkeys.HostID, hostID,
	)

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, status int, body any) {
	httpserver.NoStoreJSON(ctx, logger, w, status, body)
}

func writeErr(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, status int, code string) {
	httpserver.NoStoreJSON(ctx, logger, w, status, map[string]string{"error": code})
}
