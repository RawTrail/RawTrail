package operator

import (
	"context"
	"errors"
	"net/http"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/httpserver"
	identityapi "github.com/fleetdm/edr/server/identity/api"
)

// AgentSessionLister is the AI agent sessions list surface (issue #1283). mysql.Store satisfies it.
type AgentSessionLister interface {
	ListAgentSessions(ctx context.Context, filter api.AgentSessionFilter, cursor string, limit int) (api.AgentSessionResult, error)
}

func (h *Handler) registerAgentSessionRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/agent-sessions", h.handleListAgentSessions)
}

// SetAgentSessions installs the sessions list surface. Bootstrap wires it with the detection store in ModeFull; unset, the route
// answers 503, like the other read surfaces.
func (h *Handler) SetAgentSessions(l AgentSessionLister) { h.agentSessions = l }

// handleListAgentSessions serves GET /api/agent-sessions: AI agent sessions across the fleet, most recently active first, filtered by
// ?host_id=, ?agent= and the ?from=/?to= window they were active in, keyset-paged by ?cursor= as the fleet search is. Gated on process
// read fleet-wide, the grant the process search uses, since a session is a set of processes.
func (h *Handler) handleListAgentSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionProcessRead, identityapi.Resource{Type: "process"}) {
		return
	}
	if h.agentSessions == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	q := r.URL.Query()
	fromNs, okFrom := parseNonNegativeParam(q, "from")
	toNs, okTo := parseNonNegativeParam(q, "to")
	if !okFrom || !okTo || (fromNs > 0 && toNs > 0 && fromNs > toNs) {
		h.writeError(ctx, w, http.StatusBadRequest, errBadWindow)
		return
	}
	filter := api.AgentSessionFilter{HostID: q.Get("host_id"), AgentName: q.Get("agent"), FromNs: fromNs, ToNs: toNs}
	result, err := h.agentSessions.ListAgentSessions(ctx, filter, q.Get("cursor"), parseSearchLimit(q))
	if err != nil {
		if errors.Is(err, api.ErrInvalidCursor) {
			h.writeError(ctx, w, http.StatusBadRequest, errInvalidCursor)
			return
		}
		h.logger.ErrorContext(ctx, "list agent sessions", "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	if result.Rows == nil {
		result.Rows = []api.AgentSessionSummary{} // the UI iterates rows unconditionally
	}
	h.writeJSON(w, r, result)
}
