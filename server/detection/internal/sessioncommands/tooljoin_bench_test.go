package sessioncommands

import (
	"fmt"
	"testing"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// BenchmarkJoinToolCallsLargeTie joins the most calls a session read takes, all one turn's identical calls, with more commands than
// calls (issue #1485).
func BenchmarkJoinToolCallsLargeTie(b *testing.B) {
	const n = 20_000
	for b.Loop() {
		b.StopTimer()
		calls := make([]visibilityapi.AgentToolCall, n)
		for i := range calls {
			calls[i] = call(fmt.Sprintf("toolu_%d", i), "ls", 1_000*ms)
		}
		commands := make([]Command, n+500)
		for i := range commands {
			commands[i] = command(fmt.Sprintf("g_%d", i), "ls", 1_010*ms+int64(i))
		}
		b.StartTimer()
		JoinToolCalls(commands, calls)
	}
}
