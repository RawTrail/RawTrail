package sessioncommands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

func turnCall(id, tool, promptID string, at int64) visibilityapi.AgentToolCall {
	return visibilityapi.AgentToolCall{ToolUseID: id, ToolName: tool, PromptID: promptID, CalledAtNs: at, UID: 501, Command: id}
}

func conversationCommand(label string, start int64, m *ToolCallMatch) Command {
	return Command{Key: "row:" + label, Command: label, StartNs: start, ToolCall: m}
}

func turnIDs(turns []Turn) []string {
	out := make([]string, 0, len(turns))
	for _, t := range turns {
		out = append(out, t.PromptID)
	}
	return out
}

// spec:server-rest-api/agent-session-conversation/each-prompt-lists-what-the-agent-did-for-it
// spec:server-rest-api/agent-session-conversation/what-belongs-to-no-prompt-is-kept-not-dropped
func TestBuildConversation(t *testing.T) {
	t.Parallel()
	bashLs := turnCall("toolu_ls", "Bash", "p1", 110)
	bashGit := turnCall("toolu_git", "Bash", "p2", 210)
	edit := turnCall("toolu_edit", "Edit", "p2", 220)
	tiedA, tiedB := turnCall("toolu_a", "Bash", "p2", 230), turnCall("toolu_b", "Bash", "p2", 231)
	splitA, splitB := turnCall("toolu_c", "Bash", "p1", 120), turnCall("toolu_d", "Bash", "p3", 320)
	early := turnCall("toolu_early", "Read", "", 50)
	uncaptured := turnCall("toolu_uncaptured", "Grep", "p3", 300)
	// A later command of the uncaptured turn is read first; the turn is still at its earliest item.
	bashLate := turnCall("toolu_late", "Bash", "p3", 340)
	commands := []Command{
		conversationCommand("ls", 111, matched(bashLs)),
		conversationCommand("git status", 211, matched(bashGit)),
		conversationCommand("make", 232, &ToolCallMatch{Kind: MatchAmbiguous, Candidates: []visibilityapi.AgentToolCall{tiedA, tiedB}}),
		conversationCommand("split", 322, &ToolCallMatch{Kind: MatchAmbiguous, Candidates: []visibilityapi.AgentToolCall{splitA, splitB}}),
		conversationCommand("daemon", 400, nil),
		conversationCommand("late", 341, matched(bashLate)),
	}
	calls := []visibilityapi.AgentToolCall{bashLs, bashGit, edit, tiedA, tiedB, splitA, splitB, early, uncaptured, bashLate}
	prompts := []visibilityapi.AgentPrompt{{PromptID: "p2", Text: "check git", AtNs: 200}, {PromptID: "p1", Text: "list", AtNs: 100}}

	turns := BuildConversation(commands, calls, prompts)
	require.Equal(t, []string{"p1", "p2", "p3", ""}, turnIDs(turns), "by time, what belongs to no prompt last")

	p1, p2, p3, outside := turns[0], turns[1], turns[2], turns[3]
	assert.Equal(t, "list", p1.Prompt.Text)
	assert.Equal(t, []string{"ls"}, labels(p1.Commands))
	assert.Empty(t, p1.Calls, "a Bash call its command stands for is not listed again")

	assert.Equal(t, []string{"git status", "make"}, labels(p2.Commands), "tied calls of one turn place their command in it")
	assert.Equal(t, []visibilityapi.AgentToolCall{edit}, p2.Calls, "a call that ran no command is listed under its turn")

	assert.Nil(t, p3.Prompt, "a turn whose prompt was not captured still shows")
	assert.Equal(t, int64(300), p3.AtNs, "at its earliest item, the call, though its later command was read first")
	assert.Equal(t, []string{"late"}, labels(p3.Commands))
	assert.Equal(t, []visibilityapi.AgentToolCall{uncaptured}, p3.Calls)

	assert.Equal(t, []string{"split", "daemon"}, labels(outside.Commands), "tied calls of different turns, and no call at all")
	assert.Equal(t, []visibilityapi.AgentToolCall{early}, outside.Calls, "a call read before any turn")
}

func TestBuildConversation_NoOutsideTurnWhenEverythingHasOne(t *testing.T) {
	t.Parallel()
	call := turnCall("toolu_ls", "Bash", "p1", 110)
	turns := BuildConversation([]Command{conversationCommand("ls", 111, matched(call))}, []visibilityapi.AgentToolCall{call}, nil)
	assert.Equal(t, []string{"p1"}, turnIDs(turns))
}

// spec:server-rest-api/agent-session-conversation/a-search-keeps-the-turns-it-finds
func TestSearchConversation(t *testing.T) {
	t.Parallel()
	turns := []Turn{
		{PromptID: "p1", Prompt: &visibilityapi.AgentPrompt{Text: "Deploy the API"}, Commands: []Command{}},
		{PromptID: "p2", Commands: []Command{{Command: "kubectl apply"}}},
		{PromptID: "p3", Calls: []visibilityapi.AgentToolCall{{FilePath: "/repo/DEPLOY.md"}}},
		{PromptID: "p4", Prompt: &visibilityapi.AgentPrompt{Text: ""}, Commands: []Command{{Command: "ls"}}},
	}
	assert.Equal(t, []string{"p1", "p3"}, turnIDs(SearchConversation(turns, " deploy ")), "prompt text and a call's path")
	assert.Equal(t, []string{"p2"}, turnIDs(SearchConversation(turns, "KUBECTL")), "a command")
	assert.Len(t, SearchConversation(turns, ""), 4)
	assert.Empty(t, SearchConversation(turns, "nothing holds this"))
}

// Following ConversationPageOf from the start to the end reads every turn once, in order, whatever the page size, the turn of no
// prompt last.
//
// spec:server-rest-api/agent-session-conversation/turns-page-in-order
func TestConversationPageOf_ReadsEveryTurnOnce(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 12).Draw(rt, "n")
		var calls []visibilityapi.AgentToolCall
		for i := range n {
			promptID := rapid.SampledFrom([]string{"", "pa", "pb", "pc", "pd"}).Draw(rt, "prompt")
			calls = append(calls, turnCall("toolu_"+string(rune('a'+i)), "Read", promptID, rapid.Int64Range(0, 3).Draw(rt, "at")))
		}
		turns := BuildConversation(nil, calls, nil)
		limit := rapid.IntRange(1, 4).Draw(rt, "limit")
		read := []string{}
		cursor := ""
		for range len(turns) + 1 {
			page, err := ConversationPageOf(turns, cursor, limit)
			require.NoError(rt, err)
			require.Equal(rt, len(turns), page.TotalMatched)
			read = append(read, turnIDs(page.Turns)...)
			if cursor = page.NextCursor; cursor == "" {
				break
			}
			require.NotEmpty(rt, page.Turns, "a cursor leads to a page with something on it")
		}
		require.Equal(rt, turnIDs(turns), read)
	})
	_, err := ConversationPageOf([]Turn{}, "!!", 2)
	assert.Error(t, err)
}
