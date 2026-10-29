//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/catchup"
	rulesapi "github.com/rawtrail/rawtrail/server/rules/api"
	rulesbootstrap "github.com/rawtrail/rawtrail/server/rules/bootstrap"
	"github.com/rawtrail/rawtrail/server/rules/internal/watchedpaths"
)

// fakeCommandHistory stands in for the response context: it records what the catch-up queues and answers LatestOfType from it.
type fakeCommandHistory struct {
	mu     sync.Mutex
	latest map[string]rulesapi.HostCommand
	queued []string
}

func (f *fakeCommandHistory) insert(_ context.Context, hostIDs []string, _ string, payload []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range hostIDs {
		f.latest[h] = rulesapi.HostCommand{Payload: payload, Status: catchup.StatusPending, CreatedAt: time.Now()}
		f.queued = append(f.queued, h)
	}
	return len(hostIDs), nil
}

func (f *fakeCommandHistory) list(_ context.Context, _ string, hostIDs []string) (map[string]rulesapi.HostCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]rulesapi.HostCommand{}
	for _, h := range hostIDs {
		if c, ok := f.latest[h]; ok {
			out[h] = c
		}
	}
	return out, nil
}

func (f *fakeCommandHistory) takeQueued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := f.queued
	f.queued = nil
	return q
}

// spec:server-admin-surface/hosts-that-miss-the-watched-path-push-get-the-set/a-host-enrolled-after-a-change-gets-the-set
func TestWatchedPathsConverge_QueuesTheSetForHostsThatMissedIt(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
	enrolled := []rulesapi.HostEnrollment{
		{HostID: "host-a", EnrolledAt: time.Now().Add(-48 * time.Hour)},
		{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)},
	}
	converger := watchedpaths.NewConverger(watchedpaths.NewStore(r.db), history.insert,
		func(context.Context) ([]rulesapi.HostEnrollment, error) { return enrolled, nil }, history.list, slog.Default())

	queued, err := converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Zero(t, queued, "nothing is sent while the set has never been changed")

	put := r.do(t, http.MethodPut, watchedPathsRoute, map[string]any{"paths": []rulesapi.WatchedPath{startupItems}, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	// host-a was enrolled at the change and took the push; late-host enrolled afterwards and has nothing.
	for _, c := range r.inserter.snapshot() {
		history.latest[c.HostID] = rulesapi.HostCommand{Payload: c.Payload, Status: catchup.StatusCompleted, CreatedAt: time.Now()}
	}

	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, queued)
	assert.Equal(t, []string{"late-host"}, history.takeQueued())
	var sent rulesapi.SetWatchedPathsPayload
	require.NoError(t, json.Unmarshal(history.latest["late-host"].Payload, &sent))
	stored := r.watchedPaths(t)
	assert.Equal(t, stored.Version, sent.Version)
	assert.Equal(t, stored.UpdatedAt.UnixMicro(), sent.Epoch, "a caught-up host gets the same set, epoch included, as the push")
	assert.Equal(t, append(slices.Clone(rulesapi.DefaultWatchedPaths), startupItems), sent.Paths, "the defaults, then the operator's")

	queued, err = converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Zero(t, queued, "a host whose copy is pending is not sent another")
}

// spec:server-admin-surface/hosts-that-miss-the-watched-path-push-get-the-set/an-expired-or-reinstalled-host-gets-the-set-again
func TestWatchedPathsConverge_ResendsAfterExpiryAndReenrollment(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a", "host-b"})
	put := r.do(t, http.MethodPut, watchedPathsRoute, map[string]any{"paths": []rulesapi.WatchedPath{startupItems}, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
	pushedAt := time.Now()
	for _, c := range r.inserter.snapshot() {
		status := catchup.StatusCompleted
		if c.HostID == "host-a" {
			status = catchup.StatusExpired
		}
		history.latest[c.HostID] = rulesapi.HostCommand{Payload: c.Payload, Status: status, CreatedAt: pushedAt}
	}
	// host-b took the set, then reinstalled and enrolled again, which removed the extension's persisted copy.
	enrolled := []rulesapi.HostEnrollment{
		{HostID: "host-a", EnrolledAt: pushedAt.Add(-time.Hour)},
		{HostID: "host-b", EnrolledAt: pushedAt.Add(time.Minute)},
	}
	converger := watchedpaths.NewConverger(watchedpaths.NewStore(r.db), history.insert,
		func(context.Context) ([]rulesapi.HostEnrollment, error) { return enrolled, nil }, history.list, slog.Default())

	queued, err := converger.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, queued)
	assert.ElementsMatch(t, []string{"host-a", "host-b"}, history.takeQueued())
}

// A sweep that cannot read the enrollments or the command history queues nothing and reports why, so the loop logs it and retries.
func TestWatchedPathsConverge_SurfacesAFailedRead(t *testing.T) {
	t.Parallel()
	r := newAppControlRig(t, []string{"host-a"})
	put := r.do(t, http.MethodPut, watchedPathsRoute, map[string]any{"paths": []rulesapi.WatchedPath{startupItems}, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	readFailed := errors.New("read failed")
	enrolled := func(context.Context) ([]rulesapi.HostEnrollment, error) {
		return []rulesapi.HostEnrollment{{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)}}, nil
	}

	cases := []struct {
		name        string
		enrollments rulesapi.HostEnrollmentLister
		latest      rulesapi.HostCommandLister
	}{
		{
			name:        "enrollments",
			enrollments: func(context.Context) ([]rulesapi.HostEnrollment, error) { return nil, readFailed },
			latest:      (&fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}).list,
		},
		{
			name:        "command history",
			enrollments: enrolled,
			latest: func(context.Context, string, []string) (map[string]rulesapi.HostCommand, error) {
				return nil, readFailed
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
			converger := watchedpaths.NewConverger(watchedpaths.NewStore(r.db), history.insert, tc.enrollments, tc.latest, slog.Default())

			queued, err := converger.Converge(t.Context())
			require.ErrorIs(t, err, readFailed)
			assert.Zero(t, queued)
			assert.Empty(t, history.takeQueued())
		})
	}
}

// The catch-up is wired into the rules context's background loops: with the enrollments and command history supplied, running the
// context queues the set for a host that missed it, without any caller invoking the sweep.
func TestWatchedPathsConverge_RunsAsARulesContextLoop(t *testing.T) {
	t.Parallel()
	history := &fakeCommandHistory{latest: map[string]rulesapi.HostCommand{}}
	r := newAppControlRig(t, []string{"host-a"}, func(d *rulesbootstrap.Deps) {
		d.CommandBatchInserter = history.insert
		d.HostEnrollments = func(context.Context) ([]rulesapi.HostEnrollment, error) {
			return []rulesapi.HostEnrollment{{HostID: "late-host", EnrolledAt: time.Now().Add(-time.Hour)}}, nil
		}
		d.HostLatestCommands = history.list
		d.WatchedPathConvergeInterval = 20 * time.Millisecond
	})
	put := r.do(t, http.MethodPut, watchedPathsRoute, map[string]any{"paths": []rulesapi.WatchedPath{startupItems}, "reason": "r"})
	put.Body.Close()
	require.Equal(t, http.StatusOK, put.StatusCode)
	history.takeQueued() // the push itself went to the rig's enrolled host

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.rules.Run(ctx); close(done) }()
	require.Eventually(t, func() bool {
		history.mu.Lock()
		defer history.mu.Unlock()
		_, ok := history.latest["late-host"]
		return ok
	}, 5*time.Second, 20*time.Millisecond)
	cancel()
	<-done
}
