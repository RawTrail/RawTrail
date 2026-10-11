// Package revocationset is the per-replica revocation view both token kinds check on their auth hot path: host tokens in the
// endpoint context and service-account access tokens in the identity context.
//
// A signed token proves it is authentic and unexpired with a local check, but not that it is still allowed: an operator may have
// revoked its principal or cycled its credentials since it was minted. A Snapshot answers that from a small map of the principals
// that are revoked or have a bumped epoch, reloaded from the database on a ticker. Both contexts need exactly this, and it lives
// here rather than in either one because ADR-0004 forbids importing one context's internals from another (issue #1226).
//
// Per ADR-0010 a Snapshot is a per-replica perf cache, safe to lose: a fresh replica rebuilds it from the database on startup and
// on every refresh, and holds no state a peer needs. Revocation is therefore eventually consistent across replicas, bounded by the
// refresh interval.
package revocationset

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Entry is one principal's revocation state. Revoked is terminal (the principal is cut off regardless of epoch); Epoch is its
// current epoch, against which a presented token's epoch is compared. ID is a host ID or a service-account client ID.
type Entry struct {
	ID      string
	Epoch   int64
	Revoked bool
}

// Source loads the principals that are revoked or have a bumped epoch. Each context's store implements it.
type Source interface {
	RevocationEntries(ctx context.Context) ([]Entry, error)
}

// Snapshot is a refreshable, concurrent-read view of revocation state.
type Snapshot struct {
	src    Source
	logger *slog.Logger
	// name is the noun the refresh-failure log line uses, such as "revocation snapshot".
	name string
	// onRefreshFailure, when set, runs on every failed refresh, so a context can count failures in its own metric.
	onRefreshFailure func(context.Context)

	mu      sync.RWMutex
	entries map[string]Entry

	lastRefreshUnix atomic.Int64
}

// New constructs a snapshot over src. It starts empty (allows everything) until the first Refresh; callers should Refresh once
// synchronously before serving so a cold replica does not accept an already-revoked token. onRefreshFailure may be nil.
func New(src Source, logger *slog.Logger, name string, onRefreshFailure func(context.Context)) *Snapshot {
	if src == nil {
		panic("revocationset.New: Source must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Snapshot{src: src, logger: logger, name: name, onRefreshFailure: onRefreshFailure, entries: map[string]Entry{}}
}

// Allowed reports whether a token presenting tokenEpoch for id is currently allowed. A principal absent from the snapshot is allowed
// (never revoked, never cycled). A revoked principal is never allowed. Otherwise the token must carry an epoch at least the
// principal's current epoch.
func (s *Snapshot) Allowed(id string, tokenEpoch int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return true
	}
	if e.Revoked {
		return false
	}
	return tokenEpoch >= e.Epoch
}

// Size returns the number of principals currently in the snapshot (revoked or epoch-bumped).
func (s *Snapshot) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// LastRefresh returns when the snapshot last refreshed successfully, and false if it never has.
func (s *Snapshot) LastRefresh() (time.Time, bool) {
	last := s.lastRefreshUnix.Load()
	if last == 0 {
		return time.Time{}, false
	}
	return time.Unix(last, 0), true
}

// Observe records a principal's state on this replica so a freshly issued token is accepted immediately, without waiting for the
// next refresh. It sets the entry to {epoch, not revoked}, so a token below that epoch stays rejected with no staleness window
// while a token at it verifies. A principal at epoch 0 carries no revocation state, so it is dropped from the map. Best-effort and
// per-replica: a concurrent refresh that read an older row can momentarily disagree, and the next refresh reconciles to the DB.
func (s *Snapshot) Observe(id string, epoch int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch <= 0 {
		delete(s.entries, id)
		return
	}
	s.entries[id] = Entry{ID: id, Epoch: epoch, Revoked: false}
}

// Refresh reloads the snapshot from the source. On error the previous snapshot is retained (stale beats empty: dropping to empty
// would briefly un-revoke every cut-off principal) and the failure is logged and reported to onRefreshFailure.
func (s *Snapshot) Refresh(ctx context.Context) error {
	rows, err := s.src.RevocationEntries(ctx)
	if err != nil {
		if s.onRefreshFailure != nil {
			s.onRefreshFailure(ctx)
		}
		s.logger.WarnContext(ctx, s.name+" refresh failed; serving previous snapshot", "err", err)
		return err
	}
	next := make(map[string]Entry, len(rows))
	for _, r := range rows {
		next[r.ID] = r
	}
	s.mu.Lock()
	s.entries = next
	s.mu.Unlock()
	s.lastRefreshUnix.Store(time.Now().Unix())
	return nil
}

// Run refreshes once immediately, then on every interval tick until ctx is cancelled. A failed refresh is logged but not fatal: the
// ticker retries, and an empty snapshot fails open (allows tokens) only until the first successful load, the same availability
// posture the rest of the auth path takes on a transient DB blip.
func (s *Snapshot) Run(ctx context.Context, interval time.Duration) {
	_ = s.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Refresh(ctx)
		}
	}
}
