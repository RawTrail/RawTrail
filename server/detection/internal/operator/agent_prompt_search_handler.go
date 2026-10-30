package operator

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rawtrail/rawtrail/server/httpserver"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

const (
	// promptSearchDefaultWindow is how far back a prompt search looks when it names no window, and promptSearchMaxWindow the widest
	// window it may name (issue #1428): a search reads every host's prompts in its window, so no search is unbounded.
	promptSearchDefaultWindow = 7 * 24 * time.Hour
	promptSearchMaxWindow     = 30 * 24 * time.Hour
	// promptSearchMinRunes is the shortest query: the prompt text's skip index is over trigrams, so a shorter one would read every
	// prompt in the window. promptSearchMaxBytes is the longest, which the audit row's target holds whole.
	promptSearchMinRunes = 3
	promptSearchMaxBytes = 255

	errInvalidQuery = "invalid_query"
)

// promptSearchResourceType is what a prompt search is authorized and audited as: the search, named by its query, so the audit trail
// records what was searched for.
const promptSearchResourceType = "agent_prompt_search"

// handleSearchAgentPrompts serves GET /api/agent-prompts: the prompts whose text holds ?q=, across every host or ?host_id='s, made
// within ?from= and ?to= (ns since epoch; the last seven days by default, at most thirty), newest first, a page at a time by ?cursor=
// (issue #1428). It requires agent_prompt.read, and each search is audited with its query.
func (h *Handler) handleSearchAgentPrompts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	if utf8.RuneCountInString(query) < promptSearchMinRunes || len(query) > promptSearchMaxBytes {
		h.writeError(ctx, w, http.StatusBadRequest, errInvalidQuery)
		return
	}
	res := identityapi.Resource{Type: promptSearchResourceType, ID: query}
	if !identityapi.HTTPGate(ctx, w, h.authz, h.logger, identityapi.ActionAgentPromptRead, res) {
		return
	}
	if h.agentToolCalls == nil {
		h.writeError(ctx, w, http.StatusServiceUnavailable, errInternal)
		return
	}
	fromNs, toNs, ok := promptSearchWindow(q, time.Now())
	if !ok {
		h.writeError(ctx, w, http.StatusBadRequest, errBadWindow)
		return
	}
	page, err := h.agentToolCalls.SearchAgentPrompts(ctx, visibilityapi.AgentPromptSearch{
		Query: query, HostID: q.Get("host_id"), FromNs: fromNs, ToNs: toNs, Cursor: q.Get("cursor"), Limit: parseSearchLimit(q),
	})
	if err != nil {
		if errors.Is(err, visibilityapi.ErrInvalidPromptCursor) {
			h.writeError(ctx, w, http.StatusBadRequest, errInvalidCursor)
			return
		}
		h.logger.ErrorContext(ctx, "search agent prompts", "err", err)
		h.writeError(ctx, w, http.StatusInternalServerError, errInternal)
		return
	}
	// Prompt text is what a person typed, so no browser or proxy keeps a copy of it.
	httpserver.NoStoreJSON(ctx, h.logger, w, http.StatusOK, page)
}

// promptSearchWindow is a search's window in ns from ?from= and ?to=: to defaults to now and from to a week before to. It is not ok
// when either bound is not a non-negative integer, from is after to, or the window is wider than promptSearchMaxWindow.
func promptSearchWindow(q url.Values, now time.Time) (int64, int64, bool) {
	fromNs, okFrom := parseNonNegativeParam(q, "from")
	toNs, okTo := parseNonNegativeParam(q, "to")
	if !okFrom || !okTo {
		return 0, 0, false
	}
	if toNs == 0 {
		toNs = now.UnixNano()
	}
	if fromNs == 0 {
		fromNs = toNs - promptSearchDefaultWindow.Nanoseconds()
	}
	return fromNs, toNs, fromNs <= toNs && toNs-fromNs <= promptSearchMaxWindow.Nanoseconds()
}
