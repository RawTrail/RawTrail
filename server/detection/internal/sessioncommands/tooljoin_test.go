package sessioncommands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// cutAt is a Bash call of one turn recording text cut to at most limit bytes, on a UTF-8 boundary, as the agent cuts it.
func cutAt(text string, limit int) visibilityapi.AgentToolCall {
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	c := call("toolu_cut", text[:limit], 1_000*ms)
	c.Truncated = true
	return c
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

	// spec:web-ui/tool-calls-beside-their-processes/repeated-identical-commands-pair-in-order
	t.Run("repeated identical commands pair in order, as the edr-dev session ran ls, pwd, ls", func(t *testing.T) {
		t.Parallel()
		calls := []visibilityapi.AgentToolCall{call("toolu_a", "ls", 1_000*ms), call("toolu_b", "pwd", 2_000*ms), call("toolu_c", "ls", 3_000*ms)}
		got := joined([]Command{command("g1", "ls", 1_069*ms), command("g2", "pwd", 2_005*ms), command("g3", "ls", 3_025*ms)}, calls...)
		assert.Equal(t, map[string]*ToolCallMatch{"g1": matched(calls[0]), "g2": matched(calls[1]), "g3": matched(calls[2])}, got)
	})

	// spec:web-ui/tool-calls-beside-their-processes/another-user-s-transcript-cannot-label-a-session
	t.Run("a call pairs only with a command its transcript's owner ran", func(t *testing.T) {
		t.Parallel()
		genuine, forged := call("toolu_real", "ls", 1_000*ms), call("toolu_forged", "ls", 1_005*ms)
		forged.UID = 502
		assert.Equal(t, map[string]*ToolCallMatch{"g": matched(genuine)}, joined([]Command{command("g", "ls", 1_010*ms)}, genuine, forged))
		assert.Empty(t, joined([]Command{command("g", "ls", 1_010*ms)}, forged))
	})

	// spec:web-ui/tool-calls-beside-their-processes/calls-that-cannot-be-told-apart-are-marked-ambiguous
	t.Run("tied calls label only as many commands as they hold, and are marked ambiguous", func(t *testing.T) {
		t.Parallel()
		calls := []visibilityapi.AgentToolCall{call("toolu_a", "ls", 1_000*ms), call("toolu_b", "ls", 1_000*ms)}
		got := joined([]Command{command("g1", "ls", 1_010*ms), command("g2", "ls", 1_012*ms), command("g3", "ls", 9_000*ms)}, calls...)
		ambiguous := &ToolCallMatch{Kind: MatchAmbiguous, Candidates: calls, CandidateCount: 2, tied: calls}
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

	// spec:server-rest-api/tied-calls-label-no-more-commands-than-they-hold/overlapping-cut-calls-label-two-commands-at-most
	t.Run("two cut calls of one turn, one a prefix of the other, label two commands at most", func(t *testing.T) {
		t.Parallel()
		// The agent cuts a long command to 8 KiB, on a UTF-8 boundary: each é is two bytes, so a cut never splits one.
		long := "printf '" + strings.Repeat("é", 6000) + "'"
		short, longer := cutAt(long, 8192-1), cutAt(long, 8192+500)
		require.True(t, strings.HasPrefix(longer.Command, short.Command), "the shorter cut is a prefix of the longer")
		require.True(t, utf8.ValidString(short.Command) && utf8.ValidString(longer.Command))
		short.ToolUseID, longer.ToolUseID = "toolu_short", "toolu_longer"
		// Both commands ran whole; the second diverges between the two cuts, so only the shorter matches it.
		diverged := short.Command + "x" + long[len(short.Command)+1:]
		commands := []Command{
			command("both", long, 1_010*ms), command("short only", diverged, 1_020*ms), command("both again", long, 1_030*ms),
		}
		got := joined(commands, short, longer)
		assert.Equal(t, MatchAmbiguous, got["both"].Kind)
		assert.Equal(t, matched(short), got["short only"])
		assert.Nil(t, got["both again"], "the tie's first command is the longer call, so no third command takes it")
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

// assignable reports whether every labelled command can be given a call of its own from its label: the call it matched, or one of
// the calls of its tie. It is a maximum bipartite matching by augmenting paths, small enough for the property's sessions.
func assignable(commands []Command) bool {
	owner := map[callKey]int{}
	var try func(i int, seen map[callKey]bool) bool
	options := func(m *ToolCallMatch) []visibilityapi.AgentToolCall {
		if m.Kind == MatchMatched {
			return []visibilityapi.AgentToolCall{*m.Call}
		}
		return m.tied
	}
	try = func(i int, seen map[callKey]bool) bool {
		for _, c := range options(commands[i].ToolCall) {
			k := callKey{c.ToolUseID, c.UID}
			if seen[k] {
				continue
			}
			seen[k] = true
			if j, taken := owner[k]; !taken || try(j, seen) {
				owner[k] = i
				return true
			}
		}
		return false
	}
	for i := range commands {
		if commands[i].ToolCall != nil && !try(i, map[callKey]bool{}) {
			return false
		}
	}
	return true
}

// Over random sessions with whole and cut calls whose texts overlap, every labelled command can be given a call of its own: no label
// is one another command already took (issue #1442).
//
// spec:server-rest-api/tied-calls-label-no-more-commands-than-they-hold/overlapping-cut-calls-label-two-commands-at-most
func TestJoinToolCalls_LabelsCanBeGivenDistinctCalls(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Texts share prefixes, so a cut call can match commands of several texts.
		texts := []string{"aa", "aaaa", "aaaabb"}
		var calls []visibilityapi.AgentToolCall
		at := int64(0)
		for i := range rapid.IntRange(2, 6).Draw(t, "calls") {
			at += int64(rapid.IntRange(0, 1).Draw(t, "turn")) * 1_000 * ms
			c := call(fmt.Sprintf("toolu_%d", i), rapid.SampledFrom(texts).Draw(t, "text"), at)
			c.Truncated = rapid.Bool().Draw(t, "cut")
			calls = append(calls, c)
		}
		var commands []Command
		for i := range rapid.IntRange(0, 10).Draw(t, "commands") {
			// Shortly after the last turn, where a tie and its overlapping calls are all still in reach.
			start := at + int64(rapid.IntRange(1, 100).Draw(t, "start"))*ms
			commands = append(commands, command(fmt.Sprintf("g_%d", i), rapid.SampledFrom(texts).Draw(t, "command"), start))
		}
		slices.SortStableFunc(commands, func(a, b Command) int { return cmp.Compare(a.StartNs, b.StartNs) })
		JoinToolCalls(commands, calls)
		require.True(t, assignable(commands), "the labels can be given distinct calls")
		for _, c := range commands {
			if c.ToolCall == nil {
				continue
			}
			for _, call := range append([]visibilityapi.AgentToolCall{}, c.ToolCall.tied...) {
				require.True(t, strings.HasPrefix(c.Command, call.Command), "a tied call ran the command's text")
			}
		}
	})
}

// A transcript is its user's to write, so a turn can hold thousands of identical calls: the join labels as many commands as there
// are calls and leaves the rest unlabelled.
func TestJoinToolCalls_AThousandsCallTieLabelsAsManyCommands(t *testing.T) {
	t.Parallel()
	const n = 2_000
	calls := make([]visibilityapi.AgentToolCall, n)
	for i := range calls {
		calls[i] = call(fmt.Sprintf("toolu_%d", i), "ls", 1_000*ms)
	}
	commands := make([]Command, n+50)
	for i := range commands {
		commands[i] = command(fmt.Sprintf("g_%d", i), "ls", 1_010*ms+int64(i))
	}
	JoinToolCalls(commands, calls)
	labelled := 0
	for _, c := range commands {
		if c.ToolCall != nil {
			labelled++
		}
	}
	assert.Equal(t, n, labelled)
	assert.Nil(t, commands[n].ToolCall, "a command past the calls is left unlabelled")
}

// A search cut short by its budget may have missed a call the nearest calls could still give, so the command is left unlabelled
// rather than given an older call.
func TestJoinToolCalls_ASearchCutShortLeavesTheCommandUnlabelled(t *testing.T) {
	t.Parallel()
	older := call("toolu_older", "ls", 500*ms)
	first, second := call("toolu_a", "ls", 1_000*ms), call("toolu_b", "ls", 1_000*ms)
	commands := []Command{command("g1", "ls", 1_010*ms), command("g2", "ls", 1_020*ms), command("g3", "ls", 1_030*ms)}
	joinWithBudget(commands, []visibilityapi.AgentToolCall{older, first, second}, 1)
	for _, c := range commands[:2] {
		require.NotNil(t, c.ToolCall, c.Key)
		assert.Equal(t, MatchAmbiguous, c.ToolCall.Kind)
	}
	assert.Nil(t, commands[2].ToolCall, "not the older call, which a full search might have shown was not the nearest")
}

// Calls of one text at their own times, then as many commands of it: each command pairs with the newest call left, and its search
// does not rescan the calls the commands before it took (a matched command's call is skipped at once), so the join stays quadratic.
func TestJoinToolCalls_ManyCallsAtTheirOwnTimesPairNewestFirst(t *testing.T) {
	t.Parallel()
	const n = 3_000
	calls := make([]visibilityapi.AgentToolCall, 0, n)
	commands := make([]Command, 0, n)
	for i := range n {
		calls = append(calls, call(fmt.Sprintf("toolu_%d", i), "ls", int64(i)*ms))
		commands = append(commands, command(fmt.Sprintf("g_%d", i), "ls", int64(n+i)*ms))
	}
	start := time.Now()
	got := joined(commands, calls...)
	assert.Less(t, time.Since(start), 5*time.Second)
	for i := range n {
		require.Equal(t, matched(calls[n-1-i]), got[fmt.Sprintf("g_%d", i)])
	}
}
