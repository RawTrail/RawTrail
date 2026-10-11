package revocationset

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	entries []Entry
	err     error
	calls   int
}

func (f *fakeSource) RevocationEntries(context.Context) ([]Entry, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func newTestSnapshot(src Source) *Snapshot { return New(src, nil, "test snapshot", nil) }

func TestSnapshot_Allowed(t *testing.T) {
	t.Parallel()
	src := &fakeSource{entries: []Entry{
		{ID: "revoked", Revoked: true, Epoch: 2},
		{ID: "cycled", Revoked: false, Epoch: 2},
	}}
	s := newTestSnapshot(src)
	require.NoError(t, s.Refresh(t.Context()))

	cases := []struct {
		name       string
		id         string
		tokenEpoch int64
		want       bool
	}{
		{"absent principal allowed", "unknown", 0, true},
		{"revoked principal denied at any epoch", "revoked", 99, false},
		{"cycled principal denied below current epoch", "cycled", 1, false},
		{"cycled principal allowed at current epoch", "cycled", 2, true},
		{"cycled principal allowed above current epoch", "cycled", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, s.Allowed(tc.id, tc.tokenEpoch))
		})
	}
}

func TestSnapshot_ColdSnapshotAllowsEverything(t *testing.T) {
	t.Parallel()
	s := newTestSnapshot(&fakeSource{entries: []Entry{{ID: "revoked", Revoked: true}}})
	assert.True(t, s.Allowed("revoked", 0), "fail-open until the first load")
	assert.Equal(t, 0, s.Size())
	_, refreshed := s.LastRefresh()
	assert.False(t, refreshed)
}

func TestSnapshot_Refresh(t *testing.T) {
	t.Parallel()

	t.Run("a successful refresh replaces the view and stamps the time", func(t *testing.T) {
		t.Parallel()
		src := &fakeSource{entries: []Entry{{ID: "a", Revoked: true}, {ID: "b", Epoch: 1}}}
		s := newTestSnapshot(src)
		before := time.Now().Truncate(time.Second)
		require.NoError(t, s.Refresh(t.Context()))
		assert.Equal(t, 2, s.Size())
		last, refreshed := s.LastRefresh()
		require.True(t, refreshed)
		assert.False(t, last.Before(before), "LastRefresh is the time of this refresh")

		src.entries = []Entry{{ID: "b", Epoch: 1}}
		require.NoError(t, s.Refresh(t.Context()))
		assert.True(t, s.Allowed("a", 0), "a principal dropped from the source is dropped from the view")
		assert.Equal(t, 1, s.Size())
	})

	// spec:agent-enrollment/host-token-revocation-is-enforced-per-replica/snapshot-refresh-failure-retains-the-previous-view
	t.Run("a failed refresh retains the previous view and reports the failure", func(t *testing.T) {
		t.Parallel()
		src := &fakeSource{entries: []Entry{{ID: "revoked", Revoked: true}}}
		var failures int
		s := New(src, nil, "test snapshot", func(context.Context) { failures++ })
		require.NoError(t, s.Refresh(t.Context()))
		lastGood, _ := s.LastRefresh()

		// Dropping to empty here would briefly un-revoke every cut-off principal.
		src.err = errors.New("db down")
		require.Error(t, s.Refresh(t.Context()))
		assert.False(t, s.Allowed("revoked", 0), "previous view retained on refresh error")
		assert.Equal(t, 1, failures, "the failure hook runs once per failed refresh")
		last, _ := s.LastRefresh()
		assert.Equal(t, lastGood, last, "a failed refresh does not move the last successful refresh")
	})
}

func TestSnapshot_Observe(t *testing.T) {
	t.Parallel()

	t.Run("epoch 0 clears a stale revoked entry", func(t *testing.T) {
		t.Parallel()
		s := newTestSnapshot(&fakeSource{entries: []Entry{{ID: "h", Revoked: true}}})
		require.NoError(t, s.Refresh(t.Context()))
		s.Observe("h", 0)
		assert.True(t, s.Allowed("h", 0), "allowed without waiting for a refresh")
		assert.Equal(t, 0, s.Size())
	})

	t.Run("a bumped epoch accepts the new token and still rejects an older one", func(t *testing.T) {
		t.Parallel()
		s := newTestSnapshot(&fakeSource{entries: []Entry{{ID: "h", Epoch: 2}}})
		require.NoError(t, s.Refresh(t.Context()))
		s.Observe("h", 2)
		assert.True(t, s.Allowed("h", 2), "the current-epoch token verifies immediately")
		assert.False(t, s.Allowed("h", 1), "a token below the epoch stays rejected with no window")
		assert.Equal(t, 1, s.Size(), "the principal is kept so the older token keeps failing")
	})
}

func TestSnapshot_Run(t *testing.T) {
	t.Parallel()
	src := &fakeSource{entries: []Entry{{ID: "revoked", Revoked: true}}}
	s := newTestSnapshot(src)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, time.Hour); close(done) }()
	// Run refreshes on entry, so the revoked principal is visible without waiting an hour for the first tick.
	require.Eventually(t, func() bool { return s.Size() == 1 }, time.Second, 10*time.Millisecond)
	assert.False(t, s.Allowed("revoked", 0))
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestNew_NilSourcePanics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { New(nil, nil, "test snapshot", nil) })
}
