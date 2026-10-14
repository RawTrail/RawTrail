package operator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/httpserver"
	identityapi "github.com/fleetdm/edr/server/identity/api"
	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

// AgentSessionLister is the AI agent sessions list surface (issue #1283). mysql.Store satisfies it.
type AgentSessionLister interface {
	ListAgentSessions(ctx context.Context, filter api.AgentSessionFilter, cursor string, limit int) (api.AgentSessionResult, error)
}

func (h *Handler) registerAgentSessionRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/agent-sessions", h.handleListAgentSessions)
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls", h.handleAgentToolCalls)
}

// AgentToolCallReader is the read of the tool calls a session recorded (issue #1288), from the event archive. mysql.Store satisfies
// it.
type AgentToolCallReader interface {
	AgentToolCalls(ctx context.Context, filter visibilityapi.AgentToolCallFilter) ([]visibilityapi.AgentToolCall, bool, error)
}

// SetAgentToolCalls installs the tool-call read. Unset, the route answers 503.
func (h *Handler) SetAgentToolCalls(r AgentToolCallReader) { h.agentToolCalls = r }

const (
	// maxToolCalls bounds the calls one read lists, the same bound as the session's process read.
	maxToolCalls = maxTimelineChain
	// toolCallMargin widens the session's span when reading its calls. A session's span runs from its first marked process to its last,
	// and a call comes before its process, by as long as the user took to approve it.
	toolCallMargin = time.Hour
)

// agentToolCallsResponse is a session's tool calls, oldest first, and whether the session recorded more than the read lists.
type agentToolCallsResponse struct {
	ToolCalls []visibilityapi.AgentToolCall `json:"tool_calls"`
	Truncated bool                          `json:"truncated"`
}

// handleAgentToolCalls serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls: the tool calls the session recorded in
// its transcript, which the session view pairs with the processes each started. Gated on process read for the host, like the session's
// processes.
func (h *Handler) handleAgentToolCalls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hostID := r.PathValue("host_id")
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionProcessRead, identityapi.Resource{Type: "process", ID: hostID}) {
		return
	}
	if h.agentSessions == nil || h.agentToolCalls == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	sessionID := r.PathValue("session_id")
	sessions, err := h.agentSessions.ListAgentSessions(ctx, api.AgentSessionFilter{HostID: hostID, AgentSessionID: sessionID}, "", 1)
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent session", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	if len(sessions.Rows) == 0 {
		h.writeError(ctx, w, http.StatusNotFound, errNotFound)
		return
	}
	session := sessions.Rows[0]
	calls, truncated, err := h.agentToolCalls.AgentToolCalls(ctx, visibilityapi.AgentToolCallFilter{
		HostID: hostID, SessionID: sessionID, Limit: maxToolCalls,
		FromNs: session.FirstSeenNs - toolCallMargin.Nanoseconds(), ToNs: session.LastSeenNs + toolCallMargin.Nanoseconds(),
	})
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent tool calls", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	if calls == nil {
		calls = []visibilityapi.AgentToolCall{}
	}
	h.writeJSON(w, r, agentToolCallsResponse{ToolCalls: calls, Truncated: truncated})
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
	filter := api.AgentSessionFilter{
		HostID: q.Get("host_id"), AgentSessionID: q.Get("session"), AgentName: q.Get("agent"), FromNs: fromNs, ToNs: toNs,
	}
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
