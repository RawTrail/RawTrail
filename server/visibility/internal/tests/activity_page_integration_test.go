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

// An activity list pages by cursor (issue #1424): the rollup's cut list names where the next page starts, and reading every page in
// turn lists the class's rows once, in the rollup's order, rows first seen at the same time ordered by the rest of their key.
//
// spec:server-rest-api/agent-session-activity-pages/an-activity-list-pages-by-cursor
func TestEventArchive_ActivityPages(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	// The pidversions run the other way from the pids, so an order that swapped the two would read the rows out of order.
	gen := func(pid int) string { return `"pid":` + strconv.Itoa(pid) + `,"pidversion":` + strconv.Itoa(1000-pid) }
	var events []visibilityapi.Event
	for i := range 4 {
		// Two artifacts per time, so the order falls through to the artifact; each touched by both processes at once, so it falls
		// through to the pid.
		at := base + int64(i/2)*int64(time.Second)
		n := strconv.Itoa(i % 2)
		for _, pid := range []int{300, 301} {
			id := strconv.Itoa(i) + "-" + strconv.Itoa(pid)
			events = append(events,
				activityEvent("f"+id, "h1", "file_activity", at, `{`+gen(pid)+`,"operation":"modify","path":"/r/f`+n+`"}`),
				activityEvent("n"+id, "h1", "network_connect", at, `{`+gen(pid)+`,"protocol":"tcp","direction":"outbound",`+
					`"remote_address":"10.0.0.`+n+`","remote_port":443}`),
				activityEvent("d"+id, "h1", "dns_query", at, `{`+gen(pid)+`,"query_name":"n`+n+`.example","query_type":"A"}`))
		}
	}
	require.NoError(t, arch.Insert(ctx, events))
	gens := []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 700}, {PID: 301, PIDVersion: 699}}
	whole := visibilityapi.ActivityFilter{HostID: "h1", Generations: gens, Limit: 100}
	var all visibilityapi.ActivityRollup
	require.Eventually(t, func() bool {
		var err error
		all, err = arch.ActivityForGenerations(ctx, whole)
		return err == nil && all.DNS.Events == 8
	}, 5*time.Second, 100*time.Millisecond)
	require.Empty(t, all.Files.NextCursor, "an uncut list names no next page")

	// Pages of three put a page boundary between the two processes of one artifact, where only the pid orders them.
	small := whole
	small.Limit = 3
	first, err := arch.ActivityForGenerations(ctx, small)
	require.NoError(t, err)
	require.NotEmpty(t, first.Files.NextCursor)

	t.Run("files", func(t *testing.T) {
		t.Parallel()
		rows, cursor := first.Files.Rows, first.Files.NextCursor
		for cursor != "" {
			page, err := arch.ActivityPage(ctx, small, visibilityapi.ActivityFiles, cursor)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page.Files), 3)
			rows, cursor = append(rows, page.Files...), page.NextCursor
		}
		assert.Equal(t, all.Files.Rows, rows)
	})
	t.Run("network", func(t *testing.T) {
		t.Parallel()
		rows, cursor := first.Network.Rows, first.Network.NextCursor
		for cursor != "" {
			page, err := arch.ActivityPage(ctx, small, visibilityapi.ActivityNetwork, cursor)
			require.NoError(t, err)
			rows, cursor = append(rows, page.Network...), page.NextCursor
		}
		assert.Equal(t, all.Network.Rows, rows)
	})
	t.Run("dns", func(t *testing.T) {
		t.Parallel()
		rows, cursor := first.DNS.Rows, first.DNS.NextCursor
		for cursor != "" {
			page, err := arch.ActivityPage(ctx, small, visibilityapi.ActivityDNS, cursor)
			require.NoError(t, err)
			rows, cursor = append(rows, page.DNS...), page.NextCursor
		}
		assert.Equal(t, all.DNS.Rows, rows)
	})
	t.Run("a cursor of another class, a malformed one or an unknown class is rejected", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct{ class, cursor string }{
			{visibilityapi.ActivityDNS, first.Files.NextCursor},
			{visibilityapi.ActivityFiles, "!!"},
			{visibilityapi.ActivityFiles, "e30"}, // {}
			{"processes", ""},
		} {
			_, err := arch.ActivityPage(ctx, small, tc.class, tc.cursor)
			assert.ErrorIs(t, err, visibilityapi.ErrInvalidActivityCursor, tc.class)
		}
	})
	t.Run("no cursor reads the first page", func(t *testing.T) {
		t.Parallel()
		page, err := arch.ActivityPage(ctx, small, visibilityapi.ActivityFiles, "")
		require.NoError(t, err)
		assert.Equal(t, first.Files.Rows, page.Files)
	})
}
