package operator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	// promptSearchMinRunes is the shortest query, since a shorter one matches most prompts. promptSearchMaxBytes is the longest, which
	// the audit row's target holds whole.
	promptSearchMinRunes = 3
	promptSearchMaxBytes = 255

	errInvalidQuery = "invalid_query"
)

// promptSearchResourceType is what a prompt search is authorized and audited as: the search, named by its query, so the audit trail
// records what was searched for.
const promptSearchResourceType = "agent_prompt_search"

// handleSearchAgentPrompts serves GET /api/agent-prompts: the prompts whose text holds ?q=, across every host or ?host_id='s, made
// within ?from= and ?to= (ns since epoch; the last seven days by default, at most thirty), newest first, a page at a time by ?cursor=
// (issue #1428), which carries the window its first page resolved. It requires agent_prompt.read, and each search is audited with its query.
func (h *Handler) handleSearchAgentPrompts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("q"))
	// Text that is not UTF-8 would fail to be recorded as the audit row's target, and the search would go unaudited.
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < promptSearchMinRunes || len(query) > promptSearchMaxBytes {
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
	// A later page reads the window its first page resolved, so "the last week" does not move under a search being paged.
	window, after, err := decodePromptSearchCursor(q.Get("cursor"))
	if err != nil {
		h.writeError(ctx, w, http.StatusBadRequest, errInvalidCursor)
		return
	}
	if window == (promptSearchBounds{}) {
		var ok bool
		if window, ok = promptSearchWindow(q, time.Now()); !ok {
			h.writeError(ctx, w, http.StatusBadRequest, errBadWindow)
			return
		}
	}
	page, err := h.agentToolCalls.SearchAgentPrompts(ctx, visibilityapi.AgentPromptSearch{
		Query: query, HostID: q.Get("host_id"), FromNs: window.FromNs, ToNs: window.ToNs, Cursor: after, Limit: parseSearchLimit(q),
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
	if page.NextCursor != "" {
		page.NextCursor = encodePromptSearchCursor(window, page.NextCursor)
	}
	// Prompt text is what a person typed, so no browser or proxy keeps a copy of it.
	httpserver.NoStoreJSON(ctx, h.logger, w, http.StatusOK, page)
}

// promptSearchBounds is a search's window in ns, as its first page resolved it.
type promptSearchBounds struct {
	FromNs int64 `json:"f"`
	ToNs   int64 `json:"t"`
}

// promptSearchCursor is a prompt search's continuation: its window and the archive's own cursor after the last prompt listed.
type promptSearchCursor struct {
	promptSearchBounds
	After string `json:"a"`
}

func encodePromptSearchCursor(window promptSearchBounds, after string) string {
	raw, err := json.Marshal(promptSearchCursor{promptSearchBounds: window, After: after})
	if err != nil {
		panic(err) // unreachable: two integers and a string always marshal
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodePromptSearchCursor reads a continuation, or returns zero bounds and no cursor for none. A token that does not decode, has no
// archive cursor, or carries a window promptSearchWindow would not have resolved is an error: the token is the client's to send, so
// its window is bounded as a named one is.
func decodePromptSearchCursor(token string) (promptSearchBounds, string, error) {
	if token == "" {
		return promptSearchBounds{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return promptSearchBounds{}, "", fmt.Errorf("prompt search cursor: %w", err)
	}
	var c promptSearchCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return promptSearchBounds{}, "", fmt.Errorf("prompt search cursor: %w", err)
	}
	if c.After == "" || !c.bounded() {
		return promptSearchBounds{}, "", errors.New("prompt search cursor: no archive cursor, or a window out of bounds")
	}
	return c.promptSearchBounds, c.After, nil
}

// promptSearchWindow is a search's window in ns from ?from= and ?to=: to defaults to now and from to a week before to. It is not ok
// when either bound is not a non-negative integer, from is after to, or the window is wider than promptSearchMaxWindow.
func promptSearchWindow(q url.Values, now time.Time) (promptSearchBounds, bool) {
	fromNs, okFrom := parseNonNegativeParam(q, "from")
	toNs, okTo := parseNonNegativeParam(q, "to")
	if !okFrom || !okTo {
		return promptSearchBounds{}, false
	}
	if toNs == 0 {
		toNs = now.UnixNano()
	}
	if fromNs == 0 {
		fromNs = toNs - promptSearchDefaultWindow.Nanoseconds()
	}
	window := promptSearchBounds{FromNs: fromNs, ToNs: toNs}
	return window, window.bounded()
}

// bounded reports whether a window is one a search may read: not negative, not inverted, ending after the epoch, and no wider
// than promptSearchMaxWindow.
func (b promptSearchBounds) bounded() bool {
	return b.FromNs >= 0 && b.ToNs > 0 && b.FromNs <= b.ToNs && b.ToNs-b.FromNs <= promptSearchMaxWindow.Nanoseconds()
}
