package eventlog_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/testdb"
	"github.com/fleetdm/edr/server/visibility/api"
	"github.com/fleetdm/edr/server/visibility/internal/eventlog"
	"github.com/fleetdm/edr/server/visibility/testkit"
)

// spec:observability-instrumentation/pipeline-lag-and-backlog-are-measured/backlog-count-saturates-at-its-cap
//
// The pending count stops at its cap rather than counting a whole backlog (issue #1218): an exact count of millions of rows outran
// the gauge's collection budget. Below the cap it is exact, and it counts in-flight rows (claimed, not yet acknowledged) as pending.
func TestCountPending_StopsAtTheCap(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := testdb.Open(t)
	require.NoError(t, testkit.ApplySchema(ctx, db))
	store, err := eventlog.New(db)
	require.NoError(t, err)

	events := make([]api.Event, 5)
	for i := range events {
		events[i] = api.Event{EventID: fmt.Sprintf("e%d", i), HostID: "h1", TimestampNs: int64(i + 1), IngestedAtNs: 1,
			EventType: "exec", Payload: []byte(`{}`)}
	}
	require.NoError(t, store.Append(ctx, events))
	_, _, err = store.ClaimForHost(ctx, "h1", 2)
	require.NoError(t, err)

	cases := []struct {
		name  string
		limit int64
		want  int64
	}{
		{"below the backlog the count stops at the cap", 3, 3},
		{"at the backlog it is exact", 5, 5},
		{"above the backlog it is exact, in-flight rows included", 10, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := store.CountPendingUpTo(ctx, tc.limit)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	got, err := store.CountPending(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), got, "CountPending applies the production cap, far above this backlog")
}
