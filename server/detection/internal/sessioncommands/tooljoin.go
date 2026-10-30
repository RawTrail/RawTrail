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

// nearestIn returns the calls of a list not skipped, oldest first, that were made at the latest time no later than startNs and
// within the match window, that time, and where a later scan of the list resumes: below the calls it returned. It walks back from
// startNs, or from from when a scan of the list already passed over newer calls, and stops at the first time it finds.
func nearestIn(
	list []visibilityapi.AgentToolCall, from int, startNs int64, skip func(callKey) bool, ran func(visibilityapi.AgentToolCall) bool,
) ([]visibilityapi.AgentToolCall, int64, int) {
	// A command no call ran has no list in the index.
	if len(list) == 0 {
		return nil, 0, 0
	}
	if from < 0 {
		from, _ = slices.BinarySearchFunc(list, startNs+1, func(c visibilityapi.AgentToolCall, t int64) int {
			return cmp.Compare(c.CalledAtNs, t)
		})
	}
	var found []visibilityapi.AgentToolCall
	at, resume := int64(0), from
	for i := from - 1; i >= 0; i-- {
		c := list[i]
		if startNs-c.CalledAtNs > matchWindow.Nanoseconds() || (found != nil && c.CalledAtNs != at) {
			break
		}
		if skip(callKey{c.ToolUseID, c.UID}) || !ran(c) {
			continue
		}
		found, at, resume = append(found, c), c.CalledAtNs, i
	}
	slices.Reverse(found)
	return found, at, resume
}

// scan is where a command's search of its user's whole and cut calls resumes, so a command that passes over a time does not look
// at it again; -1 until the search starts.
type scan struct{ whole, cut int }

// nearestCalls returns the calls of the command's user that ran its text, before it and within the match window, made at the latest
// such time, leaving out the calls skipped: one call, or the tied calls of one turn. It moves the scan below what it returns.
func (idx *callIndex) nearestCalls(c *Command, at *scan, skip func(callKey) bool) []visibilityapi.AgentToolCall {
	uid := int64(*c.UID)
	whole, wholeAt, wholeResume := nearestIn(idx.whole[textKey{uid, c.Command}], at.whole, c.StartNs, skip,
		func(visibilityapi.AgentToolCall) bool { return true })
	cut, cutAt, cutResume := nearestIn(idx.cut[uid], at.cut, c.StartNs, skip, func(call visibilityapi.AgentToolCall) bool {
		return strings.HasPrefix(c.Command, call.Command)
	})
	switch {
	case len(cut) == 0 || (len(whole) > 0 && wholeAt > cutAt):
		at.whole = wholeResume
		return whole
	case len(whole) == 0 || cutAt > wholeAt:
		at.cut = cutResume
		return cut
	}
	at.whole, at.cut = wholeResume, cutResume
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
	joinWithBudget(commands, calls, assignBudget)
}

// joinWithBudget is JoinToolCalls with each command's search limited to budget calls.
func joinWithBudget(commands []Command, calls []visibilityapi.AgentToolCall, budget int) {
	j := joiner{budget: budget, idx: newCallIndex(calls), ties: map[string]*ToolCallMatch{}, owner: map[callKey]int{},
		options: map[int][]visibilityapi.AgentToolCall{}, heldUpTo: map[optionsHint]int{}, pinned: map[callKey]bool{}}
	for i := range commands {
		if c := &commands[i]; c.FromAgent && c.UID != nil {
			c.ToolCall = j.label(i, c)
		}
	}
}

// label is command i's match: its nearest calls that every labelled command can still have a call of its own beside, looking
// further back past the times it cannot, or nil.
func (j *joiner) label(i int, c *Command) *ToolCallMatch {
	at := scan{whole: -1, cut: -1}
	for {
		nearest := j.idx.nearestCalls(c, &at, func(k callKey) bool { return j.pinned[k] })
		if len(nearest) == 0 {
			return nil
		}
		m := j.match(nearest)
		// The commands of a tie share its calls as their options, so a large tie is held once however many commands it is offered.
		options := nearest
		if m.Kind == MatchAmbiguous {
			options = m.tied
		}
		j.options[i] = options
		spent := 0
		if j.assignFree(i, options) || j.assign(i, map[callKey]bool{}, &spent) {
			if m.Kind == MatchMatched {
				j.pinned[callKey{m.Call.ToolUseID, m.Call.UID}] = true
			}
			return m
		}
		// A search cut short may have missed a call the nearest calls could still give; an older call is not the answer then.
		if spent >= j.budget {
			return nil
		}
	}
}

// joiner pairs a session's commands with its calls. options are the calls each labelled command could be, and owner is the command
// each of those calls is kept for, so every labelled command has a call of its own.
type joiner struct {
	budget  int
	idx     *callIndex
	ties    map[string]*ToolCallMatch
	owner   map[callKey]int
	options map[int][]visibilityapi.AgentToolCall
	// pinned are the calls of matched commands, which no search can move, so a later command's scan skips them at once.
	pinned map[callKey]bool
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

// assignFree gives command i one of its options that no command holds, the common case, without searching.
func (j *joiner) assignFree(i int, options []visibilityapi.AgentToolCall) bool {
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
// frees it: an augmenting path, which keeps every command already assigned a call. Each step takes a free option before it moves a
// holder, so a free call one move away is found first. It looks at no more than the joiner's budget of calls.
func (j *joiner) assign(i int, seen map[callKey]bool, spent *int) bool {
	for _, call := range j.options[i] {
		if *spent >= j.budget {
			return false
		}
		*spent++
		k := callKey{call.ToolUseID, call.UID}
		if _, held := j.owner[k]; !held && !seen[k] {
			seen[k] = true
			j.owner[k] = i
			return true
		}
	}
	for _, call := range j.options[i] {
		if *spent >= j.budget {
			return false
		}
		*spent++
		k := callKey{call.ToolUseID, call.UID}
		if seen[k] {
			continue
		}
		seen[k] = true
		if j.assign(j.owner[k], seen, spent) {
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
