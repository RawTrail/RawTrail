package sessioncommands

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// Pairing each command of a session with the tool call that started it (issue #1288).
//
// The agent records a call in its transcript, then runs its shell. A command's text is the shell's eval argument, so a call pairs with
// a command that ran it and started after it. A call the user took minutes to approve starts its shell minutes later, so the pairing
// is by order rather than by a tight window: each command takes the nearest unclaimed call before it that ran its text. A call with
// no command (one the user refused) is passed over by the command after it, which takes a later call. One agent loop waits for a
// call's result before it makes the next, so its calls' shells start in the order the calls were made; the nearest call is the right
// one unless two loops (parallel subagents) ran the same command and the first waited for approval past the second's call.
//
// A transcript is its user's to write, so a call pairs only with a command its owner ran: another user cannot label this session's
// processes by writing lines into their own transcript.
//
// Calls the agent made in one turn share the turn's time, so identical commands in one turn cannot be told apart by time. Their
// commands are marked ambiguous, naming the candidates, rather than paired by a guess.

// matchWindow is the longest a command may start after its call: an hour of waiting for the user's approval.
const matchWindow = time.Hour

// Kinds of ToolCallMatch.
const (
	MatchMatched   = "matched"
	MatchAmbiguous = "ambiguous"
)

// maxListedCandidates is how many of a tie's calls a match lists. A transcript is its user's to write, so a tie can hold thousands of
// identical calls; the match lists the first few and counts the rest.
const maxListedCandidates = 10

// ToolCallMatch is the call a command was paired with, or the tied calls it could have been: the first maxListedCandidates of them,
// with CandidateCount counting all. Commands offered the same tie share one match.
type ToolCallMatch struct {
	Kind           string                        `json:"kind"`
	Call           *visibilityapi.AgentToolCall  `json:"call,omitempty"`
	Candidates     []visibilityapi.AgentToolCall `json:"candidates,omitempty"`
	CandidateCount int                           `json:"candidate_count,omitempty"`
	// tied is every call of the tie, of which Candidates lists the first few: the turn a tie places its command in, and the calls
	// the command stands for, are read from all of them (issue #1322).
	tied []visibilityapi.AgentToolCall
}

// callKey identifies a call as the archive does: by its tool_use_id and the owner of the transcript it was read from.
type callKey struct {
	id  string
	uid int64
}

// textKey is a call's owner and the command it ran.
type textKey struct {
	uid  int64
	text string
}

// callIndex holds a session's Bash calls oldest first, by owner and command for a call recorded whole, and by owner for a call cut
// to the agent's limit, which matches every command that begins with its text.
type callIndex struct {
	whole   map[textKey][]visibilityapi.AgentToolCall
	cut     map[int64][]visibilityapi.AgentToolCall
	claimed map[callKey]bool
}

func newCallIndex(calls []visibilityapi.AgentToolCall) *callIndex {
	idx := &callIndex{whole: map[textKey][]visibilityapi.AgentToolCall{}, cut: map[int64][]visibilityapi.AgentToolCall{},
		claimed: map[callKey]bool{}}
	for _, c := range calls {
		switch {
		case c.ToolName != "Bash" || c.Command == "":
		case c.Truncated:
			idx.cut[c.UID] = append(idx.cut[c.UID], c)
		default:
			k := textKey{c.UID, c.Command}
			idx.whole[k] = append(idx.whole[k], c)
		}
	}
	byTime := func(a, b visibilityapi.AgentToolCall) int { return cmp.Compare(a.CalledAtNs, b.CalledAtNs) }
	for k := range idx.whole {
		slices.SortStableFunc(idx.whole[k], byTime)
	}
	for k := range idx.cut {
		slices.SortStableFunc(idx.cut[k], byTime)
	}
	return idx
}

// nearestIn returns the unclaimed calls of a list, oldest first, that were made at the latest time no later than startNs and within
// the match window, and that time. It walks back from startNs, past claimed calls, and stops at the first time it finds.
func (idx *callIndex) nearestIn(list []visibilityapi.AgentToolCall, startNs int64, ran func(visibilityapi.AgentToolCall) bool) (
	[]visibilityapi.AgentToolCall, int64,
) {
	// A command no call ran has no list in the index.
	if len(list) == 0 {
		return nil, 0
	}
	end, _ := slices.BinarySearchFunc(list, startNs+1, func(c visibilityapi.AgentToolCall, t int64) int { return cmp.Compare(c.CalledAtNs, t) })
	var found []visibilityapi.AgentToolCall
	at := int64(0)
	for i := end - 1; i >= 0; i-- {
		c := list[i]
		if startNs-c.CalledAtNs > matchWindow.Nanoseconds() || (found != nil && c.CalledAtNs != at) {
			break
		}
		if idx.claimed[callKey{c.ToolUseID, c.UID}] || !ran(c) {
			continue
		}
		found, at = append(found, c), c.CalledAtNs
	}
	slices.Reverse(found)
	return found, at
}

// nearestCalls returns the unclaimed calls of the command's user that ran its text, before it and within the match window, made at
// the latest such time: one call, or the tied calls of one turn.
func (idx *callIndex) nearestCalls(c *Command) []visibilityapi.AgentToolCall {
	uid := int64(*c.UID)
	whole, wholeAt := idx.nearestIn(idx.whole[textKey{uid, c.Command}], c.StartNs, func(visibilityapi.AgentToolCall) bool { return true })
	cut, cutAt := idx.nearestIn(idx.cut[uid], c.StartNs, func(call visibilityapi.AgentToolCall) bool {
		return strings.HasPrefix(c.Command, call.Command)
	})
	switch {
	case len(cut) == 0 || (len(whole) > 0 && wholeAt > cutAt):
		return whole
	case len(whole) == 0 || cutAt > wholeAt:
		return cut
	}
	return append(whole, cut...)
}

// JoinToolCalls sets ToolCall on each command an agent's wrapper shell started that a Bash call pairs with. The commands must be in
// the order they started, as Build returns them.
func JoinToolCalls(commands []Command, calls []visibilityapi.AgentToolCall) {
	idx := newCallIndex(calls)
	// ties holds each set of tied calls once: the match its commands share, and how many commands have been offered it. A set of n
	// calls ran n commands at most, so it is claimed whole once n commands have taken it, and a later command looks past it.
	type tie struct {
		match *ToolCallMatch
		uses  int
	}
	ties := map[string]*tie{}
	for i := range commands {
		c := &commands[i]
		if !c.FromAgent || c.UID == nil {
			continue
		}
		tied := idx.nearestCalls(c)
		switch len(tied) {
		case 0:
		case 1:
			idx.claimed[callKey{tied[0].ToolUseID, tied[0].UID}] = true
			c.ToolCall = &ToolCallMatch{Kind: MatchMatched, Call: &tied[0]}
		default:
			key := tieKey(tied)
			t := ties[key]
			if t == nil {
				t = &tie{match: &ToolCallMatch{Kind: MatchAmbiguous, Candidates: tied[:min(len(tied), maxListedCandidates)],
					CandidateCount: len(tied), tied: tied}}
				ties[key] = t
			}
			c.ToolCall = t.match
			t.uses++
			if t.uses == len(tied) {
				for _, call := range tied {
					idx.claimed[callKey{call.ToolUseID, call.UID}] = true
				}
			}
		}
	}
}

// tieKey names a set of tied calls by their ids and owners.
func tieKey(tied []visibilityapi.AgentToolCall) string {
	var b strings.Builder
	for _, call := range tied {
		b.WriteString(call.ToolUseID)
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(call.UID, 10))
		b.WriteByte('\n')
	}
	return b.String()
}
