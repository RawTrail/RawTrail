package sessioncommands

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// largeTieShapes are the inputs a transcript's author can give the join that once made it quadratic (issue #1485), each at the most
// calls a session read takes: a tie of identical calls; that tie with as many unrelated calls each matched alone, interleaved; a
// tie of whole calls beside as many unrelated cut calls of the same user; and one whole call tied with many cut calls.
func largeTieShapes() map[string]func() ([]Command, []visibilityapi.AgentToolCall) {
	const n = 20_000
	return map[string]func() ([]Command, []visibilityapi.AgentToolCall){
		"identical tie": func() ([]Command, []visibilityapi.AgentToolCall) {
			calls := make([]visibilityapi.AgentToolCall, n)
			for i := range calls {
				calls[i] = call(fmt.Sprintf("toolu_%d", i), "ls", 1_000*ms)
			}
			commands := make([]Command, n+500)
			for i := range commands {
				commands[i] = command(fmt.Sprintf("g_%d", i), "ls", 1_010*ms+int64(i))
			}
			return commands, calls
		},
		"a tie interleaved with single matches": func() ([]Command, []visibilityapi.AgentToolCall) {
			var calls []visibilityapi.AgentToolCall
			for i := range n / 2 {
				calls = append(calls, call(fmt.Sprintf("tie_%d", i), "ls", 1_000*ms),
					call(fmt.Sprintf("one_%d", i), fmt.Sprintf("echo %d", i), 2_000*ms))
			}
			var commands []Command
			for i := range n / 2 {
				commands = append(commands, command(fmt.Sprintf("g_ls_%d", i), "ls", 3_000*ms+2*int64(i)),
					command(fmt.Sprintf("g_echo_%d", i), fmt.Sprintf("echo %d", i), 3_000*ms+2*int64(i)+1))
			}
			return commands, calls
		},
		"a whole tie beside unrelated cut calls": func() ([]Command, []visibilityapi.AgentToolCall) {
			var calls []visibilityapi.AgentToolCall
			for i := range n / 2 {
				calls = append(calls, call(fmt.Sprintf("tie_%d", i), "ls", 1_000*ms))
				cut := call(fmt.Sprintf("cut_%d", i), fmt.Sprintf("unrelated %d", i), 2_000*ms)
				cut.Truncated = true
				calls = append(calls, cut)
			}
			commands := make([]Command, n/2)
			for i := range commands {
				commands[i] = command(fmt.Sprintf("g_%d", i), "ls", 3_000*ms+int64(i))
			}
			return commands, calls
		},
		"one whole call tied with many cut calls": func() ([]Command, []visibilityapi.AgentToolCall) {
			calls := []visibilityapi.AgentToolCall{call("whole", "ls -la", 1_000*ms)}
			for i := range n - 1 {
				cut := call(fmt.Sprintf("cut_%d", i), "ls", 1_000*ms)
				cut.Truncated = true
				calls = append(calls, cut)
			}
			commands := make([]Command, n)
			for i := range commands {
				commands[i] = command(fmt.Sprintf("g_%d", i), "ls -la", 1_010*ms+int64(i))
			}
			return commands, calls
		},
	}
}

// Each shape a transcript's author could once make quadratic joins correctly at its full size: it labels as many commands as its
// calls can hold, no call is matched twice, and no tie labels more commands than it holds (issue #1485). The benchmark below times them.
//
// spec:server-rest-api/a-large-tie-is-joined-in-linear-time/the-largest-tie-a-session-read-takes-is-joined-at-once
func TestJoinToolCalls_LargeTieShapesJoinCorrectly(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]int{
		"identical tie":                           20_000,
		"a tie interleaved with single matches":   20_000,
		"a whole tie beside unrelated cut calls":  10_000,
		"one whole call tied with many cut calls": 20_000,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			commands, calls := largeTieShapes()[name]()
			JoinToolCalls(commands, calls)
			labelled := 0
			for _, c := range commands {
				if c.ToolCall != nil {
					labelled++
				}
			}
			assert.Equal(t, want, labelled)
			// Linear in the labels, where assignable's matching is not at this size: no call is matched twice, and no tie labels
			// more commands than it holds calls.
			matched, shares := map[callKey]bool{}, map[*ToolCallMatch]int{}
			for _, c := range commands {
				switch {
				case c.ToolCall == nil:
				case c.ToolCall.Kind == MatchMatched:
					k := callKey{c.ToolCall.Call.ToolUseID, c.ToolCall.Call.UID}
					require.False(t, matched[k], "a call is matched once")
					matched[k] = true
				default:
					shares[c.ToolCall]++
					require.LessOrEqual(t, shares[c.ToolCall], c.ToolCall.CandidateCount, "a tie labels no more commands than it holds")
				}
			}
		})
	}
}

// BenchmarkJoinToolCallsLargeTie joins each shape a transcript's author could make quadratic (issue #1485).
func BenchmarkJoinToolCallsLargeTie(b *testing.B) {
	for name, shape := range largeTieShapes() {
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				commands, calls := shape()
				b.StartTimer()
				JoinToolCalls(commands, calls)
			}
		})
	}
}
