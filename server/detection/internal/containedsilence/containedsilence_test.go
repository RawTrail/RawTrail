package containedsilence

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

var now = time.Unix(1_800_000_000, 0)

// minutesAgo is a time in ns, n minutes before now.
func minutesAgo(n int) int64 { return now.Add(-time.Duration(n) * time.Minute).UnixNano() }

// recorder keeps the alerts the check inserts, and stands in for the store's dedup key by matching on host and subject.
type recorder struct{ alerts []api.Alert }

func (r *recorder) insert(_ context.Context, a api.Alert) (bool, error) {
	for _, have := range r.alerts {
		if have.HostID == a.HostID && have.RuleID == a.RuleID && have.Subject == a.Subject {
			return false, nil
		}
	}
	r.alerts = append(r.alerts, a)
	return true, nil
}

func newCheck(contained []api.ContainedHost, seen map[string]int64, rec *recorder) *Check {
	return New(
		func(context.Context) ([]api.ContainedHost, error) { return contained, nil },
		func(context.Context, []string) (map[string]int64, error) { return seen, nil },
		rec.insert, 0, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-contained-host-silent-past-the-threshold-raises-one-alert
func TestContainedHostSilentPastTheThresholdAlertsOnce(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	c := newCheck([]api.ContainedHost{{HostID: "H1", Epoch: 42, ContainedAtNs: minutesAgo(60)}},
		map[string]int64{"H1": minutesAgo(11)}, rec)

	created, err := c.Run(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, created)
	require.Len(t, rec.alerts, 1)
	a := rec.alerts[0]
	assert.Equal(t, "H1", a.HostID)
	assert.Equal(t, RuleID, a.RuleID)
	assert.Equal(t, api.SeverityHigh, a.Severity)
	assert.Equal(t, api.AlertDispositionAlert, a.Disposition)
	assert.Equal(t, "containment:42", a.Subject, "the subject names the containment, so it alerts once per containment")
	assert.Contains(t, a.Description, "has not checked in for 11 minutes")

	created, err = c.Run(t.Context())
	require.NoError(t, err)
	assert.Zero(t, created, "a host still silent in the same containment does not alert again")
}

// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-contained-host-that-still-reports-does-not-alert
func TestSilenceIsCountedFromTheLaterOfCheckInAndContainment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		lastSeen  int64
		contained int64
		alerts    bool
	}{
		{"reporting", minutesAgo(1), minutesAgo(60), false},
		{"silent just under the threshold", minutesAgo(10), minutesAgo(60), false},
		{"silent past the threshold", minutesAgo(11), minutesAgo(60), true},
		{"already quiet when contained, contained recently", minutesAgo(120), minutesAgo(5), false},
		{"already quiet when contained, contained long ago", minutesAgo(120), minutesAgo(15), true},
		{"never checked in, contained long ago", 0, minutesAgo(15), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &recorder{}
			seen := map[string]int64{}
			if tc.lastSeen != 0 {
				seen["H1"] = tc.lastSeen
			}
			c := newCheck([]api.ContainedHost{{HostID: "H1", Epoch: 1, ContainedAtNs: tc.contained}}, seen, rec)
			_, err := c.Run(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.alerts, len(rec.alerts) == 1)
		})
	}
}

// spec:server-host-status/a-contained-host-that-stops-reporting-raises-an-alert/a-new-containment-alerts-again
func TestANewContainmentAlertsAgain(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	seen := map[string]int64{"H1": minutesAgo(30)}
	first := newCheck([]api.ContainedHost{{HostID: "H1", Epoch: 1, ContainedAtNs: minutesAgo(60)}}, seen, rec)
	_, err := first.Run(t.Context())
	require.NoError(t, err)
	again := newCheck([]api.ContainedHost{{HostID: "H1", Epoch: 2, ContainedAtNs: minutesAgo(20)}}, seen, rec)
	created, err := again.Run(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, created, "released and contained anew, the host alerts for the new containment")
	assert.Len(t, rec.alerts, 2)
}

func TestNoContainedHostsReadsNothingElse(t *testing.T) {
	t.Parallel()
	c := New(func(context.Context) ([]api.ContainedHost, error) { return nil, nil },
		func(context.Context, []string) (map[string]int64, error) {
			t.Fatal("no contained host means no last-seen read")
			return nil, nil
		},
		func(context.Context, api.Alert) (bool, error) { t.Fatal("nothing to alert on"); return false, nil },
		0, func() time.Time { return now }, nil)
	created, err := c.Run(t.Context())
	require.NoError(t, err)
	assert.Zero(t, created)
}

func TestAFailedReadIsReported(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	c := New(func(context.Context) ([]api.ContainedHost, error) { return nil, boom }, nil, nil, 0, nil, nil)
	_, err := c.Run(t.Context())
	assert.ErrorIs(t, err, boom)
}
