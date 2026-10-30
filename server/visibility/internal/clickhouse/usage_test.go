package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// FoldUsage folds one read's groups into both breakdowns, so they count the same messages: by model heaviest output first, by mode
// first seen first, each mode's span the widest of its groups.
func TestFoldUsage(t *testing.T) {
	t.Parallel()
	got := FoldUsage([]UsageGroup{
		{ModelUsage: api.ModelUsage{Model: "opus", Messages: 2, Input: 4, Output: 100, Thinking: 3}, Mode: "auto", FirstNs: 10, LastNs: 20},
		{ModelUsage: api.ModelUsage{Model: "sonnet", Messages: 1, Input: 1, Output: 300}, Mode: "auto", FirstNs: 5, LastNs: 5},
		{ModelUsage: api.ModelUsage{Model: "opus", Messages: 1, Input: 2, Output: 50, CacheRead: 9}, Mode: "plan", FirstNs: 30, LastNs: 30},
	})
	assert.Equal(t, api.AgentUsageTotals{
		Models: []api.ModelUsage{
			{Model: "sonnet", Messages: 1, Input: 1, Output: 300},
			{Model: "opus", Messages: 3, Input: 6, Output: 150, CacheRead: 9, Thinking: 3},
		},
		Modes: []api.ModeUsage{
			{Mode: "auto", Messages: 3, FirstNs: 5, LastNs: 20},
			{Mode: "plan", Messages: 1, FirstNs: 30, LastNs: 30},
		},
	}, got)
	assert.Equal(t, api.AgentUsageTotals{Models: []api.ModelUsage{}, Modes: []api.ModeUsage{}}, FoldUsage(nil))
}
