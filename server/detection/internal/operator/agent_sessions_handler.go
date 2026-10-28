package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/internal/graph"
	"github.com/rawtrail/rawtrail/server/detection/internal/sessioncommands"
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
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/commands", h.handleAgentSessionCommands)
	mux.HandleFunc("GET /api/hosts/{host_id}/agent-sessions/{session_id}/prompts", h.handleAgentSessionPrompts)
}

// AgentToolCallReader is the read of what a session's transcript recorded, from the event archive: its tool calls (issue #1288), the
// sessions forked from each other that their copied calls show (issue #1347), and the prompts the person typed (issue #1426).
// mysql.Store satisfies it.
type AgentToolCallReader interface {
	AgentToolCalls(ctx context.Context, filter visibilityapi.AgentToolCallFilter) ([]visibilityapi.AgentToolCall, bool, error)
	AgentPrompts(ctx context.Context, filter visibilityapi.AgentPromptFilter) (visibilityapi.AgentPromptPage, error)
	AgentSessionLineage(ctx context.Context, hostID, sessionID string, ownerUID int64) (visibilityapi.AgentSessionLineage, error)
}

// SetAgentToolCalls installs the tool-call read. Unset, the route answers 503.
func (h *Handler) SetAgentToolCalls(r AgentToolCallReader) { h.agentToolCalls = r }

const (
	// maxToolCalls bounds the calls one read lists, the same bound as the session's process read.
	maxToolCalls = graph.MaxScopedRead
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

// agentCommandsResponse is one page of a session's commands, in the order they started, with the cursor for the next page. Inferred
// and Services name the rows of the page that were moved under the process that asked launchd for them, as inferred launches and as
// services. Truncated reports that the session had more processes than its read lists, and ToolCallsTruncated more tool calls.
type agentCommandsResponse struct {
	Commands           []sessioncommands.Command `json:"commands"`
	NextCursor         string                    `json:"next_cursor,omitempty"`
	TotalMatched       int                       `json:"total_matched"`
	Inferred           []int64                   `json:"inferred"`
	Services           []int64                   `json:"services"`
	Truncated          bool                      `json:"truncated"`
	ToolCallsTruncated bool                      `json:"tool_calls_truncated"`
}

// handleAgentSessionCommands serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/commands: the commands a session ran, each
// paired with the tool call the agent reported for it, a page at a time, filtered by ?q= and keyset-paged by ?cursor=. The session
// is shaped into commands over its whole process read, so a page boundary cannot split a command or change which call pairs with it.
// Gated on process read for the host, like the session's processes.
func (h *Handler) handleAgentSessionCommands(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.sessionOfRequest(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	resp, err := h.sessionCommands(ctx, session, q.Get("q"), q.Get("cursor"), parseSearchLimit(q))
	if err != nil {
		if errors.Is(err, api.ErrInvalidCursor) {
			h.writeError(ctx, w, http.StatusBadRequest, errInvalidCursor)
			return
		}
		h.logger.ErrorContext(ctx, "read agent session commands", "host_id", session.HostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	h.writeJSON(w, r, resp)
}

// sessionCommands reads a session's processes and tool calls and returns one page of its commands.
func (h *Handler) sessionCommands(
	ctx context.Context, session *api.AgentSessionSummary, query, cursor string, limit int,
) (agentCommandsResponse, error) {
	logical, commands, truncated, err := h.buildSessionCommands(ctx, session)
	if err != nil {
		return agentCommandsResponse{}, err
	}
	calls, callsCut, err := h.agentToolCalls.AgentToolCalls(ctx, sessionToolCalls(session))
	if err != nil {
		return agentCommandsResponse{}, err
	}
	sessioncommands.JoinToolCalls(commands, calls)
	page, err := sessioncommands.PageOf(sessioncommands.Search(commands, query), cursor, limit)
	if err != nil {
		return agentCommandsResponse{}, err
	}
	resp := agentCommandsResponse{
		Commands: page.Commands, NextCursor: page.NextCursor, TotalMatched: page.TotalMatched, Inferred: []int64{}, Services: []int64{},
		Truncated: truncated, ToolCallsTruncated: callsCut,
	}
	for _, c := range page.Commands {
		for _, id := range c.ProcessIDs {
			if logical.Inferred[id] {
				resp.Inferred = append(resp.Inferred, id)
			}
			if logical.Services[id] {
				resp.Services = append(resp.Services, id)
			}
		}
	}
	return resp, nil
}

// handleAgentSessionPrompts serves GET /api/hosts/{host_id}/agent-sessions/{session_id}/prompts: the prompts a person typed in the
// session, oldest first, a page at a time by ?cursor=, kept by ?q= in their text (issue #1426). Gated on process read for the host
// like the rest of the session, and on agent_prompt.read for the session as well, which process read does not imply and which is
// audited on every read.
func (h *Handler) handleAgentSessionPrompts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.sessionOfRequest(w, r)
	if !ok {
		return
	}
	res := identityapi.Resource{Type: "agent_session", ID: promptResourceID(session.HostID, session.AgentSessionID)}
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionAgentPromptRead, res) {
		return
	}
	q := r.URL.Query()
	page, err := h.agentToolCalls.AgentPrompts(ctx, visibilityapi.AgentPromptFilter{
		HostID: session.HostID, SessionID: session.AgentSessionID, Cursor: q.Get("cursor"), Search: strings.TrimSpace(q.Get("q")),
		Limit: parseSearchLimit(q),
	})
	if err != nil {
		if errors.Is(err, visibilityapi.ErrInvalidPromptCursor) {
			h.writeError(ctx, w, http.StatusBadRequest, errInvalidCursor)
			return
		}
		h.logger.ErrorContext(ctx, "read agent session prompts", "host_id", session.HostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	// Prompt text is what a person typed, so no browser or proxy keeps a copy of it.
	httpserver.NoStoreJSON(ctx, h.logger, w, http.StatusOK, page)
}

// maxAuditTargetID is the length of audit_events.target_id, which records the resource a gate was asked for.
const maxAuditTargetID = 255

// promptResourceID names a session for the prompt-read gate, and so for its audit row, as "<host>/<session>". A host and session id
// can together run past the audit column, and a row that does not fit is not written, so a longer name is recorded as the SHA-256
// of it instead: bounded, and still one session's alone.
func promptResourceID(hostID, sessionID string) string {
	name := hostID + "/" + sessionID
	if len(name) <= maxAuditTargetID {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// buildSessionCommands reads a session's processes and shapes them into its commands, with what launchd started for each moved under
// it, and reports whether the read was cut.
func (h *Handler) buildSessionCommands(
	ctx context.Context, session *api.AgentSessionSummary,
) (sessioncommands.LogicalForest, []sessioncommands.Command, bool, error) {
	tree, err := h.readSession(ctx, session)
	if err != nil {
		return sessioncommands.LogicalForest{}, nil, false, err
	}
	logical, commands := commandsOf(tree)
	return logical, commands, tree.Truncated, nil
}

// readSession reads a session's processes as the activity rollup reads a session scope: as a flat forest, the whole session up to
// the scoped read's bound.
func (h *Handler) readSession(ctx context.Context, session *api.AgentSessionSummary) (api.ProcessTreeResult, error) {
	scope := api.TreeScope{AgentSessionID: session.AgentSessionID}
	return h.svc.BuildScopedTree(ctx, session.HostID, scope, graph.MaxScopedRead, true, 0)
}

// commandsOf shapes a session's process read into its commands, with what launchd started for each moved under it.
func commandsOf(tree api.ProcessTreeResult) (sessioncommands.LogicalForest, []sessioncommands.Command) {
	logical := sessioncommands.ApplyLaunchLinks(tree.Roots, tree.Links)
	return logical, sessioncommands.Build(logical.Roots)
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
