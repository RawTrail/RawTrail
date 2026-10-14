package operator

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/fleetdm/edr/server/detection/api"
	"github.com/fleetdm/edr/server/httpserver"
	identityapi "github.com/fleetdm/edr/server/identity/api"
	visibilityapi "github.com/fleetdm/edr/server/visibility/api"
)

// ActivityReader is the activity rollup read surface (issue #1285): what a set of process generations touched, from the archive, and
// the one process a process scope names, read by its row id.
type ActivityReader interface {
	ActivityForGenerations(ctx context.Context, filter visibilityapi.ActivityFilter) (visibilityapi.ActivityRollup, error)
	GetProcessByID(ctx context.Context, hostID string, id int64) (*api.Process, error)
	SessionTouches(ctx context.Context, hostID string, generation visibilityapi.ProcessGeneration) ([]visibilityapi.SessionTouch, error)
}

// SetActivity installs the activity read surface. Bootstrap wires it with the detection store in ModeFull; unset, the route answers
// 503.
func (h *Handler) SetActivity(r ActivityReader) { h.activity = r }

func (h *Handler) registerActivityRoutes(mux httpserver.Router) {
	mux.HandleFunc("GET /api/hosts/{host_id}/activity", h.handleActivity)
}

// activityProcess names a process of the scope, so a client can say which process touched each artifact: rows carry the (pid,
// pidversion) they were matched by, and this maps it to the process's row and image.
type activityProcess struct {
	ID         int64  `json:"id"`
	PID        int    `json:"pid"`
	PIDVersion uint32 `json:"pidversion"`
	Path       string `json:"path"`
}

// activityResponse is the rollup with the report of how far the scope reached and the scope's processes.
type activityResponse struct {
	visibilityapi.ActivityRollup
	Scope     timelineScope     `json:"scope"`
	Processes []activityProcess `json:"processes"`
}

// activityScopeRequest is the scope ?scope= names: one process or its whole subtree by the process's row id, or a story or session
// as the tree reads them.
type activityScopeRequest struct {
	process int64
	subtree bool
	tree    api.TreeScope
}

// parseActivityScope reads ?scope=process|subtree&process=<id>, ?scope=session&session=<id> or ?scope=story&root=<id>. A scope is
// required: without one there is nothing to total.
func parseActivityScope(q url.Values) (activityScopeRequest, bool) {
	switch q.Get("scope") {
	case "process", "subtree":
		id, err := strconv.ParseInt(q.Get("process"), 10, 64)
		return activityScopeRequest{process: id, subtree: q.Get("scope") == "subtree"}, err == nil && id > 0
	}
	tree, scoped, valid := parseTreeScope(q)
	return activityScopeRequest{tree: tree}, scoped && valid
}

func (h *Handler) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hostID := r.PathValue("host_id")
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionProcessRead, identityapi.Resource{Type: "process", ID: hostID}) {
		return
	}
	if h.activity == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	req, ok := parseActivityScope(r.URL.Query())
	if !ok {
		h.writeError(ctx, w, http.StatusBadRequest, errInvalidScope)
		return
	}
	roots, truncated, err := h.activityScopeProcesses(ctx, hostID, req)
	if err != nil {
		h.logger.ErrorContext(ctx, "resolve activity scope", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	generations, scope, matched := scopeGenerations(roots, truncated)
	filter := visibilityapi.ActivityFilter{HostID: hostID, Generations: generations}
	if req.tree.AgentSessionID != "" {
		if filter, err = h.boundAgents(ctx, req.tree.AgentSessionID, roots, filter); err != nil {
			h.logger.ErrorContext(ctx, "bound agent activity", "host_id", hostID, "err", err)
			h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
			return
		}
	}
	// A scope with no generation to match is answered with nothing, not with the host's whole activity: the totals would claim the
	// scope touched what other processes did.
	rollup, err := h.activity.ActivityForGenerations(ctx, filter)
	if err != nil {
		h.logger.ErrorContext(ctx, "activity rollup", "host_id", hostID, "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	processes := make([]activityProcess, 0, len(matched))
	for _, n := range matched {
		processes = append(processes, activityProcess{ID: n.ID, PID: n.PID, PIDVersion: *n.PIDVersion, Path: n.Path})
	}
	h.writeJSON(w, r, activityResponse{ActivityRollup: rollup, Scope: *scope, Processes: processes})
}

// activityScopeProcesses reads the scope's processes. A process alone is read by its row id. Its subtree is read from the chain
// pinned at it, whose descendants come from the story index (issue #1282), and cut at the pinned process: its ancestors did not do
// what the process did.
func (h *Handler) activityScopeProcesses(ctx context.Context, hostID string, req activityScopeRequest) ([]api.ProcessNode, bool, error) {
	switch {
	case req.process == 0:
		res, err := h.svc.BuildScopedTree(ctx, hostID, req.tree, maxTimelineChain, true, 0)
		if err != nil || req.tree.AgentSessionID == "" {
			return res.Roots, res.Truncated, err
		}
		roots, err := h.withAgentProcess(ctx, hostID, req.tree.AgentSessionID, res.Roots)
		return roots, res.Truncated, err
	case !req.subtree:
		proc, err := h.activity.GetProcessByID(ctx, hostID, req.process)
		if err != nil || proc == nil {
			return nil, false, err
		}
		return []api.ProcessNode{{Process: *proc}}, false, nil
	}
	res, err := h.svc.BuildChainTree(ctx, hostID, api.TimeRange{ToNs: math.MaxInt64}, req.process, true)
	if err != nil {
		return nil, false, err
	}
	pinned := findProcessNode(res.Roots, req.process)
	if pinned == nil {
		return nil, false, nil
	}
	return []api.ProcessNode{*pinned}, res.Truncated, nil
}

// withAgentProcess adds the agent process to a session's processes (issue #1287): the creator of a tool call's root whose pid is the
// one the session's CLAUDE_PID marker names. The agent carries no session marker of its own, so the session read never holds it, yet
// Claude Code's Read, Edit and Write run inside it. Its activity is that of its whole life, which spans only this session unless the
// agent switched sessions in one process.
func (h *Handler) withAgentProcess(ctx context.Context, hostID, sessionID string, roots []api.ProcessNode) ([]api.ProcessNode, error) {
	if h.agentSessions == nil {
		return roots, nil
	}
	sessions, err := h.agentSessions.ListAgentSessions(ctx, api.AgentSessionFilter{HostID: hostID, AgentSessionID: sessionID}, "", 1)
	if err != nil || len(sessions.Rows) == 0 || sessions.Rows[0].AgentPID == nil {
		return roots, err
	}
	agentPID := *sessions.Rows[0].AgentPID
	read := map[int64]bool{}
	for _, root := range roots {
		creator := root.ParentProcessID
		if creator == nil || read[*creator] {
			continue
		}
		read[*creator] = true
		proc, err := h.activity.GetProcessByID(ctx, hostID, *creator)
		if err != nil {
			return nil, err
		}
		if proc != nil && proc.PID == agentPID {
			roots = append(roots, api.ProcessNode{Process: *proc})
		}
	}
	return roots, nil
}

// boundAgents bounds each agent process in a session's scope to the span it served the session (issue #1319). A session's own
// processes carry its id; the agent carries none, so the processes without one are the agent rows withAgentProcess added. One agent
// process can serve several sessions in turn (/clear, /resume), and its activity belongs to each only while it served it.
func (h *Handler) boundAgents(
	ctx context.Context, sessionID string, roots []api.ProcessNode, filter visibilityapi.ActivityFilter,
) (visibilityapi.ActivityFilter, error) {
	for _, root := range roots {
		if root.AgentSessionID != nil || root.PIDVersion == nil {
			continue
		}
		gen := visibilityapi.ProcessGeneration{PID: int64(root.PID), PIDVersion: int64(*root.PIDVersion)}
		touches, err := h.activity.SessionTouches(ctx, filter.HostID, gen)
		if err != nil {
			return filter, err
		}
		span, bounded := sessionSpan(touches, sessionID)
		if !bounded {
			continue
		}
		span.ProcessGeneration = gen
		filter.Generations = slices.DeleteFunc(filter.Generations, func(g visibilityapi.ProcessGeneration) bool { return g == gen })
		filter.Spans = append(filter.Spans, span)
	}
	return filter, nil
}

// sessionSpan is the span an agent process served a session, from the transcripts it touched: from its first touch of the session's
// transcript (the process's start, for the first session it served) to its first touch of a later session's. It is not bounded when
// the process touched no other session's transcript, or never touched this one's, which leaves nothing to bound it by.
func sessionSpan(touches []visibilityapi.SessionTouch, sessionID string) (visibilityapi.GenerationSpan, bool) {
	own := slices.IndexFunc(touches, func(t visibilityapi.SessionTouch) bool { return t.SessionID == sessionID })
	if own < 0 || len(touches) < 2 {
		return visibilityapi.GenerationSpan{}, false
	}
	var span visibilityapi.GenerationSpan
	for _, t := range touches {
		switch {
		case t.SessionID == sessionID:
		case t.FirstNs < touches[own].FirstNs:
			span.FromNs = touches[own].FirstNs
		case span.ToNs == 0 || t.FirstNs < span.ToNs:
			span.ToNs = t.FirstNs
		}
	}
	return span, true
}

// findProcessNode returns the node of the process with the given row id, or nil when the forest does not hold it.
func findProcessNode(nodes []api.ProcessNode, id int64) *api.ProcessNode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if found := findProcessNode(nodes[i].Children, id); found != nil {
			return found
		}
	}
	return nil
}
