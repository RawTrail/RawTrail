package sessioncommands

import (
	"cmp"
	"slices"
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

// ToolCallMatch is the call a command was paired with, or the tied calls it could have been.
type ToolCallMatch struct {
	Kind       string                        `json:"kind"`
	Call       *visibilityapi.AgentToolCall  `json:"call,omitempty"`
	Candidates []visibilityapi.AgentToolCall `json:"candidates,omitempty"`
}

// runs reports whether a command ran a call's text. A text cut to the agent's limit matches the command that begins with it.
func runs(call visibilityapi.AgentToolCall, c *Command) bool {
	if call.Command == "" {
		return false
	}
	if call.Truncated {
		return strings.HasPrefix(c.Command, call.Command)
	}
	return c.Command == call.Command
}

// JoinToolCalls sets ToolCall on each command an agent's wrapper shell started that a Bash call pairs with. The commands must be in
// the order they started, as Build returns them.
func JoinToolCalls(commands []Command, calls []visibilityapi.AgentToolCall) {
	bash := slices.DeleteFunc(slices.Clone(calls), func(c visibilityapi.AgentToolCall) bool { return c.ToolName != "Bash" })
	claimed := map[string]bool{}
	// tieUses counts the commands each set of tied calls has been offered to: a set of n calls ran n commands at most, so it is
	// claimed whole once n commands have taken it, and a later command with the same text looks past it.
	tieUses := map[string]int{}
	for i := range commands {
		c := &commands[i]
		if !c.FromAgent || c.UID == nil {
			continue
		}
		tied := nearestCalls(c, bash, claimed)
		switch len(tied) {
		case 0:
		case 1:
			claimed[tied[0].ToolUseID] = true
			c.ToolCall = &ToolCallMatch{Kind: MatchMatched, Call: &tied[0]}
		default:
			c.ToolCall = &ToolCallMatch{Kind: MatchAmbiguous, Candidates: tied}
			claimTie(tied, tieUses, claimed)
		}
	}
}

// nearestCalls returns the unclaimed calls of the command's user that ran its text, before it and within the match window, made at
// the latest such time: one call, or the tied calls of one turn.
func nearestCalls(c *Command, bash []visibilityapi.AgentToolCall, claimed map[string]bool) []visibilityapi.AgentToolCall {
	var candidates []visibilityapi.AgentToolCall
	for _, call := range bash {
		if !claimed[call.ToolUseID] && call.UID == int64(*c.UID) && runs(call, c) && call.CalledAtNs <= c.StartNs &&
			c.StartNs-call.CalledAtNs <= matchWindow.Nanoseconds() {
			candidates = append(candidates, call)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	nearest := slices.MaxFunc(candidates, func(a, b visibilityapi.AgentToolCall) int {
		return cmp.Compare(a.CalledAtNs, b.CalledAtNs)
	}).CalledAtNs
	return slices.DeleteFunc(candidates, func(call visibilityapi.AgentToolCall) bool { return call.CalledAtNs != nearest })
}

// claimTie counts one more command offered a set of tied calls, and claims the set once it has been offered to as many commands as
// it holds calls.
func claimTie(tied []visibilityapi.AgentToolCall, tieUses map[string]int, claimed map[string]bool) {
	ids := make([]string, 0, len(tied))
	for _, call := range tied {
		ids = append(ids, call.ToolUseID)
	}
	tie := strings.Join(ids, "\n")
	tieUses[tie]++
	if tieUses[tie] == len(tied) {
		for _, call := range tied {
			claimed[call.ToolUseID] = true
		}
	}
}
