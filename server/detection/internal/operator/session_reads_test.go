package operator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// countingRead is a session read that counts its calls and answers with a forest of one process named for the call.
type countingRead struct {
	calls atomic.Int32
	err   error
}

func (r *countingRead) read(context.Context) (api.ProcessTreeResult, error) {
	n := r.calls.Add(1)
	if r.err != nil {
		return api.ProcessTreeResult{}, r.err
	}
	return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: int64(n)}}}}, nil
}

// fakeClock is a clock a test moves by hand.
type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time { return c.at }

// spec:server-rest-api/a-session-s-process-read-is-kept-briefly/a-second-view-of-an-idle-session-reuses-its-read
// spec:server-rest-api/a-session-s-process-read-is-kept-briefly/a-session-that-moved-on-is-read-again
func TestSessionReads_KeepsAReadWhileTheSessionIsIdle(t *testing.T) {
	t.Parallel()
	session := api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	cases := []struct {
		name string
		// between changes the session or the clock after the first read.
		between   func(s *api.AgentSessionSummary, clock *fakeClock)
		wantReads int32
	}{
		{name: "idle session within the window", between: func(*api.AgentSessionSummary, *fakeClock) {}, wantReads: 1},
		{name: "just inside the window", between: func(_ *api.AgentSessionSummary, c *fakeClock) {
			c.at = c.at.Add(sessionReadTTL - time.Nanosecond)
		}, wantReads: 1},
		{name: "window passed", between: func(_ *api.AgentSessionSummary, c *fakeClock) { c.at = c.at.Add(sessionReadTTL) }, wantReads: 2},
		{name: "session moved on", between: func(s *api.AgentSessionSummary, _ *fakeClock) { s.LastSeenNs = 200 }, wantReads: 2},
		{name: "another session", between: func(s *api.AgentSessionSummary, _ *fakeClock) { s.AgentSessionID = "s2" }, wantReads: 2},
		{name: "another host", between: func(s *api.AgentSessionSummary, _ *fakeClock) { s.HostID = "h2" }, wantReads: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := &fakeClock{at: time.Unix(1_000, 0)}
			reads := newSessionReads()
			reads.now = clock.now
			read := &countingRead{}
			s := session
			first, err := reads.get(t.Context(), &s, read.read)
			require.NoError(t, err)
			tc.between(&s, clock)
			second, err := reads.get(t.Context(), &s, read.read)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReads, read.calls.Load())
			if tc.wantReads == 1 {
				assert.Equal(t, first, second, "the kept read is the one answered")
			} else {
				assert.NotEqual(t, first, second, "the session is read afresh")
			}
		})
	}
}

func TestSessionReads_DoesNotKeepAFailedRead(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	failing := &countingRead{err: errors.New("mysql down")}
	_, err := reads.get(t.Context(), session, failing.read)
	require.ErrorIs(t, err, failing.err)
	ok := &countingRead{}
	tree, err := reads.get(t.Context(), session, ok.read)
	require.NoError(t, err)
	assert.Equal(t, int32(1), ok.calls.Load(), "a failed read leaves nothing kept, so the next view reads again")
	assert.Len(t, tree.Roots, 1)
}

// spec:server-rest-api/a-session-s-process-read-is-kept-briefly/views-opened-together-share-one-read
func TestSessionReads_ConcurrentViewsShareOneRead(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	release := make(chan struct{})
	var calls atomic.Int32
	read := func(context.Context) (api.ProcessTreeResult, error) {
		calls.Add(1)
		<-release
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 7}}}}, nil
	}
	const views = 8
	var wg sync.WaitGroup
	results := make([]api.ProcessTreeResult, views)
	for i := range views {
		wg.Go(func() {
			tree, err := reads.get(t.Context(), session, read)
			assert.NoError(t, err)
			results[i] = tree
		})
	}
	// Let every view reach the read before it finishes; a view that arrives after it is answered from what was kept.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	assert.Equal(t, int32(1), calls.Load())
	for _, tree := range results {
		assert.Equal(t, int64(7), tree.Roots[0].ID)
	}
}

// A caller that gives up stops waiting, while the read it started goes on for the others and is kept; the read is bounded by its own
// deadline instead, since no one request's cancel stops it.
func TestSessionReads_SharedReadOutlivesACallerThatGivesUp(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	read := func(ctx context.Context) (api.ProcessTreeResult, error) {
		calls.Add(1)
		deadline, ok := ctx.Deadline()
		assert.True(t, ok, "the shared read has a deadline of its own")
		assert.WithinDuration(t, time.Now().Add(sessionReadTimeout), deadline, 5*time.Second)
		close(started)
		<-release
		if err := ctx.Err(); err != nil {
			return api.ProcessTreeResult{}, err
		}
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 1}}}}, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	gaveUp := make(chan error, 1)
	go func() {
		_, err := reads.get(ctx, session, read)
		gaveUp <- err
	}()
	<-started
	waiting := make(chan api.ProcessTreeResult, 1)
	go func() {
		tree, err := reads.get(t.Context(), session, read)
		assert.NoError(t, err)
		waiting <- tree
	}()
	cancel()
	select {
	case err := <-gaveUp:
		require.ErrorIs(t, err, context.Canceled, "the caller that hung up stops waiting")
	case <-time.After(5 * time.Second):
		close(release)
		require.Fail(t, "the caller that hung up waited for the read to finish")
	}
	close(release)
	assert.Len(t, (<-waiting).Roots, 1, "the read the first caller started still answers the one waiting on it")
	tree, err := reads.get(t.Context(), session, read)
	require.NoError(t, err)
	assert.Len(t, tree.Roots, 1)
	assert.Equal(t, int32(1), calls.Load(), "and is kept")
}

// A view that sees the session at a later last activity does not wait on a read of it begun earlier, which could miss what moved it.
func TestSessionReads_AMovedSessionDoesNotJoinAnEarlierRead(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	release := make(chan struct{})
	started := make(chan struct{})
	earlier := func(context.Context) (api.ProcessTreeResult, error) {
		close(started)
		<-release
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 100}}}}, nil
	}
	later := func(context.Context) (api.ProcessTreeResult, error) {
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 200}}}}, nil
	}
	done := make(chan api.ProcessTreeResult, 1)
	go func() {
		tree, err := reads.get(t.Context(), &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}, earlier)
		assert.NoError(t, err)
		done <- tree
	}()
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tree, err := reads.get(ctx, &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 200}, later)
	require.NoError(t, err, "a view of the moved session does not wait for the earlier read")
	assert.Equal(t, int64(200), tree.Roots[0].ID, "read afresh, not handed the read begun before the session moved")
	close(release)
	assert.Equal(t, int64(100), (<-done).Roots[0].ID)

	// The earlier read finished last, but the session has moved past it, so the later read stays kept.
	kept, ok := reads.kept(sessionKey{hostID: "h1", sessionID: "s1"}, 200)
	require.True(t, ok)
	assert.Equal(t, int64(200), kept.Roots[0].ID)
}

// A view that missed the kept read just before another's read was kept finds it when its turn to read comes, rather than reading
// the session again: the cache is looked at again inside the shared read.
func TestSessionReads_LooksAgainBeforeReading(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	readAt := time.Unix(1_000, 0)
	reads.entries[sessionKey{hostID: "h1", sessionID: "s1"}] = sessionRead{
		tree: api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 7}}}}, lastSeenNs: 100, readAt: readAt,
	}
	// The first look finds the read expired, as one about to be replaced is; by the second, inside the shared read, it is fresh again.
	looks := 0
	reads.now = func() time.Time {
		looks++
		if looks == 1 {
			return readAt.Add(sessionReadTTL)
		}
		return readAt
	}
	read := &countingRead{}
	tree, err := reads.get(t.Context(), session, read.read)
	require.NoError(t, err)
	assert.Equal(t, int32(0), read.calls.Load())
	assert.Equal(t, int64(7), tree.Roots[0].ID)
}

func TestSessionReads_KeepsAtMostAFewReads(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{at: time.Unix(1_000, 0)}
	reads := newSessionReads()
	reads.now = clock.now
	read := &countingRead{}
	for i := range maxSessionReads + 1 {
		session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: fmt.Sprintf("s%d", i), LastSeenNs: 100}
		_, err := reads.get(t.Context(), session, read.read)
		require.NoError(t, err)
		clock.at = clock.at.Add(time.Second)
	}
	assert.Len(t, reads.entries, maxSessionReads)
	assert.NotContains(t, reads.entries, sessionKey{hostID: "h1", sessionID: "s0"}, "the oldest read makes room")
	assert.Contains(t, reads.entries, sessionKey{hostID: "h1", sessionID: fmt.Sprintf("s%d", maxSessionReads)})
}

// spec:server-rest-api/a-session-s-process-read-is-kept-briefly/a-second-view-of-an-idle-session-reuses-its-read
//
// TestAgentSessionViews_ShareTheSessionRead pins the wiring: the Timeline, the Commands view and the processes search of one idle
// session read its processes once between them.
func TestAgentSessionViews_ShareTheSessionRead(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	tree := commandsTree(false)
	svc := fakeService{buildScopedTree: func(
		ctx context.Context, hostID string, scope api.TreeScope, limit int, flatten bool, pinnedID int64,
	) (api.ProcessTreeResult, error) {
		reads.Add(1)
		return tree(ctx, hostID, scope, limit, flatten, pinnedID)
	}}
	srv := newCommandsServer(t, svc, commandsCalls(), summarySession())
	for _, path := range []string{conversationPath, commandsPath, "/api/hosts/h1/agent-sessions/47abb2a1/processes"} {
		resp := doGet(t, srv, path)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
	assert.Equal(t, int32(1), reads.Load())
}
