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
	whole map[textKey][]visibilityapi.AgentToolCall
	cut   map[int64][]visibilityapi.AgentToolCall
}

func newCallIndex(calls []visibilityapi.AgentToolCall) *callIndex {
	idx := &callIndex{whole: map[textKey][]visibilityapi.AgentToolCall{}, cut: map[int64][]visibilityapi.AgentToolCall{}}
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

// nearestIn returns the calls of a list not passed over, oldest first, that were made at the latest time no later than startNs and
// within the match window, and that time. It walks back from startNs, past the calls passed over, and stops at the first time it finds.
func nearestIn(
	list []visibilityapi.AgentToolCall, startNs int64, passed map[callKey]bool, ran func(visibilityapi.AgentToolCall) bool,
) ([]visibilityapi.AgentToolCall, int64) {
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
		if passed[callKey{c.ToolUseID, c.UID}] || !ran(c) {
			continue
		}
		found, at = append(found, c), c.CalledAtNs
	}
	slices.Reverse(found)
	return found, at
}

// nearestCalls returns the calls of the command's user that ran its text, before it and within the match window, made at the latest
// such time, leaving out the calls passed over: one call, or the tied calls of one turn.
func (idx *callIndex) nearestCalls(c *Command, passed map[callKey]bool) []visibilityapi.AgentToolCall {
	uid := int64(*c.UID)
	whole, wholeAt := nearestIn(idx.whole[textKey{uid, c.Command}], c.StartNs, passed, func(visibilityapi.AgentToolCall) bool { return true })
	cut, cutAt := nearestIn(idx.cut[uid], c.StartNs, passed, func(call visibilityapi.AgentToolCall) bool {
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
//
// Each call starts one command at most, so the join keeps a call of its own for every command it labels: a matched command's is its
// call, and the commands offered a tie share the tie's calls. A command takes its nearest calls only when every labelled command can
// still have a call of its own; otherwise it passes over them and looks further back. A tie of n calls is so used up once n commands
// have taken it, and calls whose ties overlap, as two cut calls do when one is a prefix of the other, cannot between them label more
// commands than they are (issue #1442).
func JoinToolCalls(commands []Command, calls []visibilityapi.AgentToolCall) {
	j := joiner{idx: newCallIndex(calls), ties: map[string]*ToolCallMatch{}, owner: map[callKey]int{},
		options: map[int][]visibilityapi.AgentToolCall{}, heldUpTo: map[optionsHint]int{}}
	for i := range commands {
		c := &commands[i]
		if !c.FromAgent || c.UID == nil {
			continue
		}
		passed := map[callKey]bool{}
		for {
			nearest := j.idx.nearestCalls(c, passed)
			if len(nearest) == 0 {
				break
			}
			m := j.match(nearest)
			// The commands of a tie share its calls as their options, so a large tie is held once however many commands it is offered.
			j.options[i] = nearest
			if m.Kind == MatchAmbiguous {
				j.options[i] = m.tied
			}
			spent := 0
			if j.assignFree(i) || j.assign(i, map[callKey]bool{}, &spent) {
				c.ToolCall = m
				break
			}
			for _, call := range nearest {
				passed[callKey{call.ToolUseID, call.UID}] = true
			}
		}
	}
}

// joiner pairs a session's commands with its calls. options are the calls each labelled command could be, and owner is the command
// each of those calls is kept for, so every labelled command has a call of its own.
type joiner struct {
	idx     *callIndex
	ties    map[string]*ToolCallMatch
	owner   map[callKey]int
	options map[int][]visibilityapi.AgentToolCall
	// heldUpTo is how many leading options of a list of options are already held, for the commands of a large tie, which are offered
	// the same calls one after another. A held call stays held, so the free search resumes there rather than rescanning the tie.
	heldUpTo map[optionsHint]int
}

// optionsHint names a list of options by its ends and length, cheaply. Two lists it confuses only start the free search past a free
// call, which the augmenting search still finds.
type optionsHint struct {
	first, last callKey
	n           int
}

// assignBudget is how many calls one command's search for a call of its own may look at. A transcript is its user's to write, so a
// tie can hold thousands of identical calls; past the budget the command is left unlabelled, which never labels it wrongly.
const assignBudget = 4096

// assignFree gives command i an option no command holds, the common case, without searching.
func (j *joiner) assignFree(i int) bool {
	options := j.options[i]
	hint := optionsHint{callKey{options[0].ToolUseID, options[0].UID}, callKey{options[len(options)-1].ToolUseID,
		options[len(options)-1].UID}, len(options)}
	for at := j.heldUpTo[hint]; at < len(options); at++ {
		k := callKey{options[at].ToolUseID, options[at].UID}
		if _, held := j.owner[k]; !held {
			j.owner[k] = i
			j.heldUpTo[hint] = at + 1
			return true
		}
	}
	j.heldUpTo[hint] = len(options)
	return false
}

// assign finds command i a call of its own among its options, moving a command that holds one to another of its options when that
// frees it: an augmenting path, which keeps every command already assigned a call. It looks at no more than assignBudget calls.
func (j *joiner) assign(i int, seen map[callKey]bool, spent *int) bool {
	for _, call := range j.options[i] {
		if *spent >= assignBudget {
			return false
		}
		*spent++
		k := callKey{call.ToolUseID, call.UID}
		if seen[k] {
			continue
		}
		seen[k] = true
		if holder, held := j.owner[k]; !held || j.assign(holder, seen, spent) {
			j.owner[k] = i
			return true
		}
	}
	return false
}

// match labels a command whose nearest calls are calls: the one call it matched, or the tie it shares with the other commands
// offered the same calls.
func (j *joiner) match(calls []visibilityapi.AgentToolCall) *ToolCallMatch {
	if len(calls) == 1 {
		return &ToolCallMatch{Kind: MatchMatched, Call: &calls[0]}
	}
	key := tieKey(calls)
	if m := j.ties[key]; m != nil {
		return m
	}
	m := &ToolCallMatch{Kind: MatchAmbiguous, Candidates: calls[:min(len(calls), maxListedCandidates)], CandidateCount: len(calls),
		tied: calls}
	j.ties[key] = m
	return m
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
