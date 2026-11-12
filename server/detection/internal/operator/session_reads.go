package operator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// A session's views each read the session's processes whole, up to graph.MaxScopedRead of them, and a long session's read is most of
// a page's time: on dogfood the Timeline, the Commands view and the Summary each spent about a second on it, again on every page and
// tab (issue #1550). sessionReads keeps a session's read for sessionReadTTL, while its last activity stays the same.
const (
	sessionReadTTL = 30 * time.Second
	// sessionReadTimeout bounds a shared read, which no one request's cancel stops. A response is written within 30 seconds
	// (httpserver's write timeout), so a read that takes longer serves no one.
	sessionReadTimeout = 30 * time.Second
	// maxSessionReads bounds the reads kept. A read holds at most graph.MaxScopedRead processes, so a few are tens of megabytes.
	maxSessionReads = 4
)

// sessionReads keeps recent session reads. It is a per-replica perf cache, safe to lose: a miss reads the session again, and a read
// is kept no longer than sessionReadTTL, so a replica that lost it, or never held it, serves the same session a moment later.
//
// A kept read is shared by every request that hits it, so no caller may change it: commandsOf, the Summary and the processes search
// each build their own values from it.
type sessionReads struct {
	*keptReads[sessionKey, api.ProcessTreeResult]
}

type sessionKey struct{ hostID, sessionID string }

func newSessionReads() *sessionReads {
	return &sessionReads{newKeptReads[sessionKey, api.ProcessTreeResult](maxSessionReads)}
}

// get returns the session's kept read when it is younger than sessionReadTTL and the session has done nothing since, and otherwise
// reads it with read and keeps it. A session that is still running moves its last activity as its processes arrive, so its views
// read it afresh.
func (c *sessionReads) get(
	ctx context.Context, session *api.AgentSessionSummary, read func(context.Context) (api.ProcessTreeResult, error),
) (api.ProcessTreeResult, error) {
	return c.keptReads.get(ctx, sessionKey{hostID: session.HostID, sessionID: session.AgentSessionID}, session.LastSeenNs, read)
}

// keptReads keeps up to max recent reads, each for sessionReadTTL while the version it was read at stays current. It is a per-replica
// perf cache, safe to lose: a miss reads again.
type keptReads[K comparable, V any] struct {
	mu      sync.Mutex
	max     int
	entries map[K]keptRead[V]
	// flight lets concurrent misses on one key at one version, a session view's tabs opening together, share one read.
	flight singleflight.Group
	now    func() time.Time
	// waiting, when set, is called once a caller has joined or started a shared read and is about to wait on it. Tests use it to
	// know every caller is waiting before the read finishes.
	waiting func()
}

// keptRead is one kept read, with the version it was read at and when that was.
type keptRead[V any] struct {
	value   V
	version int64
	readAt  time.Time
}

func newKeptReads[K comparable, V any](maxReads int) *keptReads[K, V] {
	return &keptReads[K, V]{max: maxReads, entries: map[K]keptRead[V]{}, now: time.Now}
}

// get returns the key's kept read when it is younger than sessionReadTTL and was read at version, and otherwise reads it with read
// and keeps it.
//
// Callers that miss on the same key at the same version share one read. The read is not canceled with the caller that started it,
// so the others waiting on it are not failed by that caller hanging up; it is bounded by sessionReadTimeout instead. A caller whose
// own request ends stops waiting.
func (c *keptReads[K, V]) get(ctx context.Context, key K, version int64, read func(context.Context) (V, error)) (V, error) {
	if value, ok := c.kept(key, version); ok {
		return value, nil
	}
	shared := context.WithoutCancel(ctx)
	flight := c.flight.DoChan(fmt.Sprintf("%v\x00%d", key, version), func() (any, error) {
		// A caller that missed just as another's read was kept finds it here rather than reading again.
		if value, ok := c.kept(key, version); ok {
			return value, nil
		}
		readCtx, cancel := context.WithTimeout(shared, sessionReadTimeout)
		defer cancel()
		// The read is as old as its start: what changed while it ran may be missing from it.
		readAt := c.now()
		value, err := read(readCtx)
		if err != nil {
			return nil, err
		}
		c.keep(key, keptRead[V]{value: value, version: version, readAt: readAt})
		return value, nil
	})
	if c.waiting != nil {
		c.waiting()
	}
	var zero V
	select {
	case res := <-flight:
		if res.Err != nil {
			return zero, res.Err
		}
		return res.Val.(V), nil
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func (c *keptReads[K, V]) kept(key K, version int64) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.version != version || c.now().Sub(e.readAt) >= sessionReadTTL {
		var zero V
		return zero, false
	}
	return e.value, true
}

// keep stores a read, dropping the expired ones and then, while the cache is full, the oldest. A read at an earlier version than the
// one kept, which finished after it, is not kept over it.
func (c *keptReads[K, V]) keep(key K, e keptRead[V]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.entries[key]; ok && kept.version > e.version {
		return
	}
	now := c.now()
	delete(c.entries, key)
	for k, kept := range c.entries {
		if now.Sub(kept.readAt) >= sessionReadTTL {
			delete(c.entries, k)
		}
	}
	for len(c.entries) >= c.max {
		var oldest K
		var oldestAt time.Time
		for k, kept := range c.entries {
			if oldestAt.IsZero() || kept.readAt.Before(oldestAt) {
				oldest, oldestAt = k, kept.readAt
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = e
}
