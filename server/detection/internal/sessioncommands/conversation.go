package sessioncommands

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/rawtrail/rawtrail/server/detection/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// Turn is one prompt of a session and what the agent did for it (issue #1322): the commands it ran and the tool calls it made that
// ran no command (a file read or edit, a web fetch, an MCP tool), joined by the turn the agent recorded each call in. The turn with no
// PromptID holds what belongs to no prompt: a command no call pairs with, or a call the agent read before any turn.
type Turn struct {
	PromptID string `json:"prompt_id,omitempty"`
	// AtNs is the prompt's time, or the turn's first command or call when its prompt was not captured.
	AtNs int64 `json:"at_ns"`
	// Prompt is the prompt that began the turn, when the agent captured it.
	Prompt   *visibilityapi.AgentPrompt    `json:"prompt,omitempty"`
	Commands []Command                     `json:"commands"`
	Calls    []visibilityapi.AgentToolCall `json:"calls"`
}

// outside reports whether the turn holds what belongs to no prompt.
func (t Turn) outside() bool { return t.PromptID == "" }

// sortKey orders the turns: by time, the turn of no prompt last whatever its time, since it gathers what came at any point.
func (t Turn) sortKey() (int64, string) {
	if t.outside() {
		return math.MaxInt64, ""
	}
	return t.AtNs, t.PromptID
}

// BuildConversation groups a session's commands, tool calls and prompts into its turns, oldest first. A command is the turn of the
// call it pairs with, or of the calls it ties with when they share one; any other command belongs to no prompt. A call that a
// command stands for is not listed again.
func BuildConversation(commands []Command, calls []visibilityapi.AgentToolCall, prompts []visibilityapi.AgentPrompt) []Turn {
	turns := map[string]*Turn{}
	turnOf := func(promptID string, atNs int64) *Turn {
		t, ok := turns[promptID]
		if !ok {
			t = &Turn{PromptID: promptID, AtNs: atNs, Commands: []Command{}, Calls: []visibilityapi.AgentToolCall{}}
			turns[promptID] = t
		}
		if t.Prompt == nil && atNs < t.AtNs {
			t.AtNs = atNs
		}
		return t
	}
	for _, p := range prompts {
		t := turnOf(p.PromptID, p.AtNs)
		t.Prompt, t.AtNs = &p, p.AtNs
	}
	covered := map[callKey]bool{}
	for _, c := range commands {
		promptID := ""
		if c.ToolCall != nil {
			promptID = matchTurn(*c.ToolCall, covered)
		}
		t := turnOf(promptID, c.StartNs)
		t.Commands = append(t.Commands, c)
	}
	for _, call := range calls {
		if covered[callKey{call.ToolUseID, call.UID}] {
			continue
		}
		t := turnOf(call.PromptID, call.CalledAtNs)
		t.Calls = append(t.Calls, call)
	}
	out := make([]Turn, 0, len(turns))
	for _, t := range turns {
		out = append(out, *t)
	}
	slices.SortFunc(out, func(a, b Turn) int {
		aAt, aID := a.sortKey()
		bAt, bID := b.sortKey()
		return cmp.Or(cmp.Compare(aAt, bAt), cmp.Compare(aID, bID))
	})
	return out
}

// matchTurn is the turn a command's call match places it in, marking the calls it stands for as covered: the matched call's turn, or
// the turn every tied candidate shares. Candidates of different turns leave the command in no prompt's.
func matchTurn(m ToolCallMatch, covered map[callKey]bool) string {
	if m.Call != nil {
		covered[callKey{m.Call.ToolUseID, m.Call.UID}] = true
		return m.Call.PromptID
	}
	shared := ""
	for i, c := range m.Candidates {
		covered[callKey{c.ToolUseID, c.UID}] = true
		if i == 0 {
			shared = c.PromptID
		} else if c.PromptID != shared {
			shared = ""
		}
	}
	return shared
}

// SearchConversation keeps the turns a query finds, case-insensitively: in the prompt's text, a command, or a call's command or path.
// An empty query keeps every turn. A prompt's text the caller cannot read is removed before the search, so it is never searched.
func SearchConversation(turns []Turn, rawQuery string) []Turn {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return turns
	}
	kept := []Turn{}
	for _, t := range turns {
		if turnMatches(t, query) {
			kept = append(kept, t)
		}
	}
	return kept
}

func turnMatches(t Turn, query string) bool {
	if t.Prompt != nil && strings.Contains(strings.ToLower(t.Prompt.Text), query) {
		return true
	}
	if slices.ContainsFunc(t.Commands, func(c Command) bool { return strings.Contains(strings.ToLower(c.Command), query) }) {
		return true
	}
	return slices.ContainsFunc(t.Calls, func(c visibilityapi.AgentToolCall) bool {
		return strings.Contains(strings.ToLower(c.Command), query) || strings.Contains(strings.ToLower(c.FilePath), query)
	})
}

// ConversationPage is one page of turns, with the cursor for the next (empty on the last) and how many turns the read matched.
type ConversationPage struct {
	Turns        []Turn `json:"turns"`
	NextCursor   string `json:"next_cursor,omitempty"`
	TotalMatched int    `json:"total_matched"`
}

// ConversationPageOf returns the limit turns after the cursor, in BuildConversation's order, or the first limit for an empty cursor.
// The cursor is the event search's (time, id) token over the turns' order; one that does not decode is an api.ErrInvalidCursor.
func ConversationPageOf(turns []Turn, token string, limit int) (ConversationPage, error) {
	limit = max(limit, 1)
	from := 0
	if token != "" {
		after, err := visibilityapi.DecodeEventCursor(token)
		if err != nil {
			return ConversationPage{}, api.ErrInvalidCursor
		}
		for from < len(turns) {
			at, id := turns[from].sortKey()
			if cmp.Or(cmp.Compare(at, after.TimestampNs), cmp.Compare(id, after.EventID)) > 0 {
				break
			}
			from++
		}
	}
	page := ConversationPage{Turns: turns[from:min(from+limit, len(turns))], TotalMatched: len(turns)}
	if from+limit < len(turns) {
		at, id := page.Turns[len(page.Turns)-1].sortKey()
		page.NextCursor = visibilityapi.EncodeEventCursor(visibilityapi.EventCursor{TimestampNs: at, EventID: id})
	}
	return page, nil
}
