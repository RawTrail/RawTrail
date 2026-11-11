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

func TestSessionReads_SharedReadOutlivesACallerThatGivesUp(t *testing.T) {
	t.Parallel()
	reads := newSessionReads()
	session := &api.AgentSessionSummary{HostID: "h1", AgentSessionID: "s1", LastSeenNs: 100}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tree, err := reads.get(ctx, session, func(ctx context.Context) (api.ProcessTreeResult, error) {
		if err := ctx.Err(); err != nil {
			return api.ProcessTreeResult{}, err
		}
		return api.ProcessTreeResult{Roots: []api.ProcessNode{{Process: api.Process{ID: 1}}}}, nil
	})
	require.NoError(t, err, "the read others may share is not canceled with the caller that started it")
	assert.Len(t, tree.Roots, 1)
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
