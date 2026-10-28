//go:build integration

package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// largeScope is how many generations the large-scope test names: the most a session's activity reads (issue #1424).
const largeScope = 20000

// A session's activity is read over every generation its process read names, up to the bound, which writes a filter far longer than
// ClickHouse's default query size allows (issue #1424).
//
// spec:server-rest-api/agent-session-reads-past-one-page/a-session-past-the-tree-page-is-read-whole
func TestEventArchive_ActivityOverALargeScope(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	gens := make([]visibilityapi.ProcessGeneration, 0, largeScope)
	var events []visibilityapi.Event
	for i := range largeScope {
		pid, pv := int64(100_000+i), int64(1_000_000+i)
		gens = append(gens, visibilityapi.ProcessGeneration{PID: pid, PIDVersion: pv})
		if i%1000 == 999 {
			events = append(events, activityEvent("q"+strconv.Itoa(i), "h1", "dns_query", base+int64(i),
				`{"pid":`+strconv.FormatInt(pid, 10)+`,"pidversion":`+strconv.FormatInt(pv, 10)+`,"query_name":"n`+strconv.Itoa(i)+`.example"}`))
		}
	}
	require.NoError(t, arch.Insert(ctx, events))

	filter := visibilityapi.ActivityFilter{HostID: "h1", Generations: gens}
	var totals visibilityapi.ActivitySummary
	require.Eventually(t, func() bool {
		var err error
		totals, err = arch.ActivityTotalsForGenerations(ctx, filter)
		require.NoError(t, err)
		return totals.DNS.Events == largeScope/1000
	}, 10*time.Second, 200*time.Millisecond)
	assert.Equal(t, int64(largeScope/1000), totals.DNS.Distinct, "the last generation's lookup is counted")

	rollup, err := arch.ActivityForGenerations(ctx, filter)
	require.NoError(t, err)
	assert.Equal(t, int64(largeScope/1000), rollup.DNS.Events)
	assert.Len(t, rollup.DNS.Rows, largeScope/1000)
}
