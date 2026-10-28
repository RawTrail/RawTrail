package sessioncommands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

const uid = 501

func command(key, text string, startNs int64) Command {
	return Command{Key: key, Command: text, FromAgent: true, UID: new(uid), StartNs: startNs, LastNs: startNs}
}

func call(id, text string, atNs int64) visibilityapi.AgentToolCall {
	return visibilityapi.AgentToolCall{ToolUseID: id, ToolName: "Bash", Command: text, CalledAtNs: atNs, UID: uid}
}

// joined pairs the commands with the calls and returns them by key.
func joined(commands []Command, calls ...visibilityapi.AgentToolCall) map[string]*ToolCallMatch {
	JoinToolCalls(commands, calls)
	out := map[string]*ToolCallMatch{}
	for _, c := range commands {
		if c.ToolCall != nil {
			out[c.Key] = c.ToolCall
		}
	}
	return out
}

func matched(c visibilityapi.AgentToolCall) *ToolCallMatch {
	return &ToolCallMatch{Kind: MatchMatched, Call: &c}
}

// spec:server-rest-api/agent-session-commands/commands-are-labelled-and-paired-with-their-tool-calls
func TestJoinToolCalls(t *testing.T) {
	t.Parallel()

	t.Run("repeated identical commands pair in order, as the edr-dev session ran ls, pwd, ls", func(t *testing.T) {
		t.Parallel()
		calls := []visibilityapi.AgentToolCall{call("toolu_a", "ls", 1_000*ms), call("toolu_b", "pwd", 2_000*ms), call("toolu_c", "ls", 3_000*ms)}
		got := joined([]Command{command("g1", "ls", 1_069*ms), command("g2", "pwd", 2_005*ms), command("g3", "ls", 3_025*ms)}, calls...)
		assert.Equal(t, map[string]*ToolCallMatch{"g1": matched(calls[0]), "g2": matched(calls[1]), "g3": matched(calls[2])}, got)
	})

	t.Run("a call pairs only with a command its transcript's owner ran", func(t *testing.T) {
		t.Parallel()
		genuine, forged := call("toolu_real", "ls", 1_000*ms), call("toolu_forged", "ls", 1_005*ms)
		forged.UID = 502
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(genuine)}, joined([]Command{command("g", "ls", 1_010*ms)}, genuine, forged))
		assert.Empty(t, joined([]Command{command("g", "ls", 1_010*ms)}, forged))
	})

	t.Run("tied calls label only as many commands as they hold, and are marked ambiguous", func(t *testing.T) {
		t.Parallel()
		calls := []visibilityapi.AgentToolCall{call("toolu_a", "ls", 1_000*ms), call("toolu_b", "ls", 1_000*ms)}
		got := joined([]Command{command("g1", "ls", 1_010*ms), command("g2", "ls", 1_012*ms), command("g3", "ls", 9_000*ms)}, calls...)
		ambiguous := &ToolCallMatch{Kind: MatchAmbiguous, Candidates: calls, CandidateCount: 2}
		assert.Equal(t, map[string]*ToolCallMatch{"g1": ambiguous, "g2": ambiguous}, got)
	})

	t.Run("a call is claimed by its id and owner, so another owner's call with the same id still pairs", func(t *testing.T) {
		t.Parallel()
		mine, theirs := call("toolu_a", "ls", 1_000*ms), call("toolu_a", "ls", 1_000*ms)
		theirs.UID = 502
		other := command("g2", "ls", 1_020*ms)
		other.UID = new(502)
		got := joined([]Command{command("g1", "ls", 1_010*ms), other}, mine, theirs)
		assert.Equal(t, map[string]*ToolCallMatch{"g1": matched(mine), "g2": matched(theirs)}, got)
	})

	t.Run("a large tie lists its first calls, counts them all, and is shared by the commands offered it", func(t *testing.T) {
		t.Parallel()
		var calls []visibilityapi.AgentToolCall
		var commands []Command
		for i := range 25 {
			calls = append(calls, call(fmt.Sprintf("toolu_%02d", i), "ls", 1_000*ms))
			commands = append(commands, command(fmt.Sprintf("g%02d", i), "ls", 1_010*ms+int64(i)*ms))
		}
		commands = append(commands, command("late", "ls", 1_100*ms))
		got := joined(commands, calls...)
		require.Len(t, got, 25, "the tie is claimed whole once 25 commands took it")
		first := got["g00"]
		assert.Equal(t, MatchAmbiguous, first.Kind)
		assert.Equal(t, 25, first.CandidateCount)
		assert.Equal(t, calls[:maxListedCandidates], first.Candidates)
		assert.Same(t, first, got["g24"], "one match, shared")
	})

	t.Run("a whole call and a cut call of one turn are tied", func(t *testing.T) {
		t.Parallel()
		long := "cat <<'EOF'\n" + strings.Repeat("x", 20) + "\nEOF"
		whole, cut := call("toolu_whole", long, 1_000*ms), call("toolu_cut", long[:10], 1_000*ms)
		cut.Truncated = true
		got := joined([]Command{command("g", long, 1_010*ms)}, whole, cut)
		assert.Equal(t, MatchAmbiguous, got["g"].Kind)
		assert.Equal(t, 2, got["g"].CandidateCount)
	})

	t.Run("between a whole call and a cut call made at different times, the nearer pairs", func(t *testing.T) {
		t.Parallel()
		long := "cat <<'EOF'\n" + strings.Repeat("x", 20) + "\nEOF"
		early, late := call("toolu_early", long, 1_000*ms), call("toolu_late", long[:10], 2_000*ms)
		late.Truncated = true
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(late)}, joined([]Command{command("g", long, 2_010*ms)}, early, late))
		earlyCut, lateWhole := call("toolu_early", long[:10], 1_000*ms), call("toolu_late", long, 2_000*ms)
		earlyCut.Truncated = true
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(lateWhole)},
			joined([]Command{command("g", long, 2_010*ms)}, earlyCut, lateWhole))
	})

	t.Run("a call pairs with one command at most", func(t *testing.T) {
		t.Parallel()
		only := call("toolu_a", "ls", 1_000*ms)
		got := joined([]Command{command("g1", "ls", 1_010*ms), command("g2", "ls", 4_010*ms)}, only)
		assert.Equal(t, map[string]*ToolCallMatch{"g1": matched(only)}, got)
	})

	t.Run("a call that ran nothing, such as one the user refused, is passed over", func(t *testing.T) {
		t.Parallel()
		approved := call("approved", "rm -rf build", 5_000*ms)
		got := joined([]Command{command("g", "rm -rf build", 5_010*ms)}, call("refused", "rm -rf build", 1_000*ms), approved)
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(approved)}, got)
	})

	t.Run("an approval is waited out, but not past an hour", func(t *testing.T) {
		t.Parallel()
		assert.Len(t, joined([]Command{command("g", "make", 30*60_000*ms)}, call("toolu_a", "make", 0)), 1)
		assert.Empty(t, joined([]Command{command("g", "make", 61*60_000*ms)}, call("toolu_a", "make", 0)))
	})

	t.Run("a cut command matches by its start, and only a Bash call pairs, only with an agent's shell after it", func(t *testing.T) {
		t.Parallel()
		long := "cat <<'EOF'\n" + strings.Repeat("x", 20) + "\nEOF"
		cut := call("toolu_cut", long[:10], 1_000*ms)
		cut.Truncated = true
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(cut)}, joined([]Command{command("g", long, 1_020*ms)}, cut))
		edit := call("toolu_edit", "ls", 1_000*ms)
		edit.ToolName = "Edit"
		assert.Empty(t, joined([]Command{command("g", "ls", 1_020*ms)}, edit))
		notAgent := command("g", "ls", 1_020*ms)
		notAgent.FromAgent = false
		assert.Empty(t, joined([]Command{notAgent}, call("toolu_a", "ls", 1_000*ms)))
		noUser := command("g", "ls", 1_020*ms)
		noUser.UID = nil
		assert.Empty(t, joined([]Command{noUser}, call("toolu_a", "ls", 1_000*ms)))
		assert.Empty(t, joined([]Command{command("g", "ls", 990*ms)}, call("toolu_a", "ls", 1_000*ms)))
		assert.Empty(t, joined([]Command{command("g", "ls", 1_020*ms)}, call("toolu_a", "", 1_000*ms)))
	})
}

// Over random sessions, a call labels one command at most, with its own text and before it starts, an ambiguous match names tied
// calls only, and when no two calls share a time every command is paired with the call that really started it.
//
// spec:server-rest-api/agent-session-commands/commands-are-labelled-and-paired-with-their-tool-calls
func TestJoinToolCalls_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		texts := []string{"ls", "pwd", "git status"}
		var calls []visibilityapi.AgentToolCall
		var commands []Command
		truth := map[string]string{}
		at := int64(0)
		for i := range rapid.IntRange(0, 12).Draw(t, "calls") {
			// Calls of one turn share its time; turns are at least a model round trip apart, longer than a shell takes to start.
			at += int64(rapid.IntRange(0, 2).Draw(t, "turn")) * 1_000 * ms
			id := fmt.Sprintf("toolu_%d", i)
			text := rapid.SampledFrom(texts).Draw(t, "text")
			calls = append(calls, call(id, text, at))
			if rapid.Float64Range(0, 1).Draw(t, "ran") < 0.8 {
				key := "g_" + id
				commands = append(commands, command(key, text, at+5*ms+int64(rapid.IntRange(0, 59).Draw(t, "delay"))*ms))
				truth[key] = id
			}
		}
		// Build returns commands in the order they started.
		slices.SortStableFunc(commands, func(a, b Command) int { return cmp.Compare(a.StartNs, b.StartNs) })
		JoinToolCalls(commands, calls)
		used := map[string]bool{}
		distinctTimes := map[int64]bool{}
		for _, c := range calls {
			distinctTimes[c.CalledAtNs] = true
		}
		for _, c := range commands {
			if c.ToolCall == nil {
				continue
			}
			if c.ToolCall.Kind == MatchAmbiguous {
				require.Greater(t, len(c.ToolCall.Candidates), 1)
				for _, candidate := range c.ToolCall.Candidates {
					require.Equal(t, c.ToolCall.Candidates[0].CalledAtNs, candidate.CalledAtNs)
				}
				continue
			}
			require.False(t, used[c.ToolCall.Call.ToolUseID], "a call labels one command at most")
			used[c.ToolCall.Call.ToolUseID] = true
			require.Equal(t, c.Command, c.ToolCall.Call.Command)
			require.LessOrEqual(t, c.ToolCall.Call.CalledAtNs, c.StartNs)
		}
		if len(distinctTimes) == len(calls) {
			for _, c := range commands {
				require.NotNil(t, c.ToolCall, "every command is paired when no two calls share a time")
				require.Equal(t, truth[c.Key], c.ToolCall.Call.ToolUseID)
			}
		}
	})
}
