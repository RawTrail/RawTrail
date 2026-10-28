package operator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/httpserver"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// AgentSessionLister is the AI agent sessions list surface (issue #1283). mysql.Store satisfies it.
type AgentSessionLister interface {
	ListAgentSessions(ctx context.Context, filter api.AgentSessionFilter, cursor string, limit int) (api.AgentSessionResult, error)
	// GetAgentSession reads one session's record without its counts; nil when the host holds no such session.
	GetAgentSession(ctx context.Context, hostID, sessionID string) (*api.AgentSessionSummary, error)
}

func (h *Handler) registerAgentSessionRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/agent-sessions", h.handleListAgentSessions)
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls", h.handleAgentToolCalls)
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage", h.handleAgentSessionLineage)
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/summary", h.handleAgentSessionSummary)
}

// AgentToolCallReader is the read of the tool calls a session recorded (issue #1288), and of the sessions forked from each other that
// their copied calls show (issue #1347), from the event archive. mysql.Store satisfies it.
type AgentToolCallReader interface {
	AgentToolCalls(ctx context.Context, filter visibilityapi.AgentToolCallFilter) ([]visibilityapi.AgentToolCall, bool, error)
	AgentSessionLineage(ctx context.Context, hostID, sessionID string, ownerUID int64) (visibilityapi.AgentSessionLineage, error)
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

// sessionOfRequest gates a session's read on process read for its host and reads the session the path names, answering the request
// itself (403, 503, 500 or 404) and returning false when there is none to serve.
func (h *Handler) sessionOfRequest(w http.ResponseWriter, r *http.Request) (*api.AgentSessionSummary, bool) {
	ctx := r.Context()
	hostID := r.PathValue("host_id")
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionProcessRead, identityapi.Resource{Type: "process", ID: hostID}) {
		return nil, false
	}
	if h.agentSessions == nil || h.agentToolCalls == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return nil, false
	}
	session, err := h.agentSessions.GetAgentSession(ctx, hostID, r.PathValue("session_id"))
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent session", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return nil, false
	}
	if session == nil {
		h.writeError(ctx, w, http.StatusNotFound, errNotFound)
		return nil, false
	}
	return session, true
}

// handleAgentSessionLineage serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/lineage: the session this one was forked from
// and the sessions forked from it, which the session view links to. Gated on process read for the host, like the session's tool calls.
func (h *Handler) handleAgentSessionLineage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.sessionOfRequest(w, r)
	if !ok {
		return
	}
	hostID, sessionID := session.HostID, session.AgentSessionID
	// The lineage is read from the owner's transcripts only; a session none of whose processes is retained has no owner to read for.
	lineage := visibilityapi.AgentSessionLineage{ForkedSessionIDs: []string{}}
	if session.OwnerUID == nil {
		h.writeJSON(w, r, lineage)
		return
	}
	lineage, err := h.agentToolCalls.AgentSessionLineage(ctx, hostID, sessionID, *session.OwnerUID)
	if err == nil {
		lineage, err = h.ownersLineage(ctx, hostID, *session.OwnerUID, lineage)
	}
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent session lineage", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	h.writeJSON(w, r, lineage)
}

// ownersLineage keeps the links to sessions the same user owns. The lineage was read from the owner's transcripts, which name any
// session they like: a user's transcript named for another user's session would otherwise list that session as a fork of theirs.
func (h *Handler) ownersLineage(
	ctx context.Context, hostID string, ownerUID int64, lineage visibilityapi.AgentSessionLineage,
) (visibilityapi.AgentSessionLineage, error) {
	owned := func(sessionID string) (bool, error) {
		linked, err := h.agentSessions.GetAgentSession(ctx, hostID, sessionID)
		return err == nil && linked != nil && linked.OwnerUID != nil && *linked.OwnerUID == ownerUID, err
	}
	kept := visibilityapi.AgentSessionLineage{ForkedSessionIDs: []string{}}
	if lineage.ForkedFromSessionID != "" {
		ok, err := owned(lineage.ForkedFromSessionID)
		if err != nil {
			return kept, err
		}
		if ok {
			kept.ForkedFromSessionID = lineage.ForkedFromSessionID
		}
	}
	for _, fork := range lineage.ForkedSessionIDs {
		ok, err := owned(fork)
		if err != nil {
			return kept, err
		}
		if ok {
			kept.ForkedSessionIDs = append(kept.ForkedSessionIDs, fork)
		}
	}
	return kept, nil
}

// handleAgentToolCalls serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/tool-calls: the tool calls the session recorded in
// its transcript, which the session view pairs with the processes each started. Gated on process read for the host, like the session's
// processes.
func (h *Handler) handleAgentToolCalls(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.sessionOfRequest(w, r)
	if !ok {
		return
	}
	calls, truncated, err := h.agentToolCalls.AgentToolCalls(ctx, sessionToolCalls(session))
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent tool calls", "host_id", session.HostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	if calls == nil {
		calls = []visibilityapi.AgentToolCall{}
	}
	h.writeJSON(w, r, agentToolCallsResponse{ToolCalls: calls, Truncated: truncated})
}

// handleAgentSessionSummary serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/summary: a session's totals, which the
// session view's counts and its Summary read rather than counting the rows a page loaded. Gated on process read for the host, like
// the session's processes and activity.
func (h *Handler) handleAgentSessionSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.sessionOfRequest(w, r)
	if !ok {
		return
	}
	if h.sessionTotals == nil || h.activity == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	hostID, sessionID := session.HostID, session.AgentSessionID
	summary, err := h.sessionSummary(ctx, session)
	if err != nil {
		h.logger.ErrorContext(ctx, "read agent session summary", "host_id", hostID, "session", sessionID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	h.writeJSON(w, r, summary)
}

// sessionToolCalls selects the tool calls of a session: those it recorded within its span widened by toolCallMargin, up to
// maxToolCalls.
func sessionToolCalls(session *api.AgentSessionSummary) visibilityapi.AgentToolCallFilter {
	return visibilityapi.AgentToolCallFilter{
		HostID: session.HostID, SessionID: session.AgentSessionID, Limit: maxToolCalls,
		FromNs: session.FirstSeenNs - toolCallMargin.Nanoseconds(), ToNs: session.LastSeenNs + toolCallMargin.Nanoseconds(),
	}
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
