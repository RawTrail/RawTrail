package bootstrap_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	"github.com/rawtrail/rawtrail/server/detection/bootstrap"
	detectiontestkit "github.com/rawtrail/rawtrail/server/detection/testkit"
	identitytestkit "github.com/rawtrail/rawtrail/server/identity/testkit"
	"github.com/rawtrail/rawtrail/server/testdb/full"
	visibilitybootstrap "github.com/rawtrail/rawtrail/server/visibility/bootstrap"
)

// alertCountingMetrics counts the alerts it is told were created. The embedded recorder is nil, so a method this test did not expect
// to be called fails loudly rather than being silently absorbed.
type alertCountingMetrics struct {
	api.MetricsRecorder
	mu      sync.Mutex
	created map[string]int
}

func (m *alertCountingMetrics) AlertCreated(_ context.Context, ruleID, _ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.created[ruleID]++
}

// QueueRowsPruned is called by the queue-prune sweep Run also starts; this test does not count it.
func (m *alertCountingMetrics) QueueRowsPruned(context.Context, int64) {}

func (m *alertCountingMetrics) count(ruleID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.created[ruleID]
}

// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-contained-host-silent-past-the-threshold-raises-one-alert
//
// TestContainedSilenceWiring_RunRaisesTheAlertAndCountsIt drives the silent-contained-host check through the real Run loop (issue
// #1228), because that is what a deployment executes: the wiring is only real if the goroutine Run starts carries the lister cmd/main
// hands in, and the alert it raises is counted in edr.alerts.created like the ones the engine raises.
func TestContainedSilenceWiring_RunRaisesTheAlertAndCountsIt(t *testing.T) {
	t.Parallel()
	db := full.Open(t)
	vis, err := visibilitybootstrap.New(visibilitybootstrap.Deps{DB: db})
	require.NoError(t, err)
	d, err := bootstrap.New(bootstrap.Deps{
		DB:           db,
		Mode:         bootstrap.ModeFull,
		AuthZ:        identitytestkit.AllowAllAuthZ{},
		EventLog:     vis.EventLog(),
		EventArchive: detectiontestkit.NewMemArchive(),
		// Long intervals keep the other loops quiet; the check runs once at start, which is all this needs. ProcessInterval has no
		// zero-default, so it must be set.
		ProcessInterval:      time.Hour,
		StaleProcessInterval: time.Hour,
		RetentionInterval:    time.Hour,
		QueuePruneInterval:   time.Hour,
	})
	require.NoError(t, err)
	metrics := &alertCountingMetrics{created: map[string]int{}}
	d.SetMetrics(metrics)

	ctx := t.Context()
	quiet := time.Now().Add(-30 * time.Minute).UnixNano()
	_, err = db.ExecContext(ctx, `INSERT INTO hosts (host_id, event_count, last_seen_ns, last_checkin_ns) VALUES (?, 1, ?, ?)`,
		"silent-host", quiet, quiet)
	require.NoError(t, err)
	d.SetContainedHosts(func(context.Context) ([]api.ContainedHost, error) {
		return []api.ContainedHost{{HostID: "silent-host", Epoch: 3, ContainedAtNs: quiet}}, nil
	})

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = d.Run(runCtx)
	}()
	require.Eventually(t, func() bool { return metrics.count("contained_host_silent") == 1 }, 10*time.Second, 20*time.Millisecond,
		"Run must start the check, and the alert it raises must be counted")
	cancel()
	<-done

	var n int
	require.NoError(t, db.GetContext(ctx, &n, `SELECT COUNT(*) FROM alerts WHERE host_id = ? AND rule_id = ?`,
		"silent-host", "contained_host_silent"))
	assert.Equal(t, 1, n)
}
