package operator

import (
	"context"
	"strconv"
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
	mu      sync.Mutex
	entries map[sessionKey]sessionRead
	// flight lets concurrent misses on one session at one last activity, a session view's tabs opening together, share one read.
	flight singleflight.Group
	now    func() time.Time
	// waiting, when set, is called once a caller has joined or started a shared read and is about to wait on it. Tests use it to
	// know every caller is waiting before the read finishes.
	waiting func()
}

type sessionKey struct{ hostID, sessionID string }

// sessionRead is one kept read, with the session's last activity when it was read and when that was.
type sessionRead struct {
	tree       api.ProcessTreeResult
	lastSeenNs int64
	readAt     time.Time
}

func newSessionReads() *sessionReads {
	return &sessionReads{entries: map[sessionKey]sessionRead{}, now: time.Now}
}

// get returns the session's kept read when it is younger than sessionReadTTL and the session has done nothing since, and otherwise
// reads it with read and keeps it. A session that is still running moves its last activity as its processes arrive, so its views
// read it afresh.
//
// Views that miss on the same session at the same last activity share one read. The read is not canceled with the caller that
// started it, so the others waiting on it are not failed by that caller hanging up; it is bounded by sessionReadTimeout instead. A
// caller whose own request ends stops waiting.
func (c *sessionReads) get(
	ctx context.Context, session *api.AgentSessionSummary, read func(context.Context) (api.ProcessTreeResult, error),
) (api.ProcessTreeResult, error) {
	key := sessionKey{hostID: session.HostID, sessionID: session.AgentSessionID}
	lastSeenNs := session.LastSeenNs
	if tree, ok := c.kept(key, lastSeenNs); ok {
		return tree, nil
	}
	shared := context.WithoutCancel(ctx)
	flight := c.flight.DoChan(key.hostID+"\x00"+key.sessionID+"\x00"+strconv.FormatInt(lastSeenNs, 10), func() (any, error) {
		// A view that missed just as another's read was kept finds it here rather than reading again.
		if tree, ok := c.kept(key, lastSeenNs); ok {
			return tree, nil
		}
		readCtx, cancel := context.WithTimeout(shared, sessionReadTimeout)
		defer cancel()
		// The read is as old as its start: what changed while it ran may be missing from it.
		readAt := c.now()
		tree, err := read(readCtx)
		if err != nil {
			return api.ProcessTreeResult{}, err
		}
		c.keep(key, sessionRead{tree: tree, lastSeenNs: lastSeenNs, readAt: readAt})
		return tree, nil
	})
	if c.waiting != nil {
		c.waiting()
	}
	select {
	case res := <-flight:
		if res.Err != nil {
			return api.ProcessTreeResult{}, res.Err
		}
		return res.Val.(api.ProcessTreeResult), nil
	case <-ctx.Done():
		return api.ProcessTreeResult{}, ctx.Err()
	}
}

func (c *sessionReads) kept(key sessionKey, lastSeenNs int64) (api.ProcessTreeResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.lastSeenNs != lastSeenNs || c.now().Sub(e.readAt) >= sessionReadTTL {
		return api.ProcessTreeResult{}, false
	}
	return e.tree, true
}

// keep stores a read, dropping the expired ones and then, while the cache is full, the oldest. A read of the session at an earlier
// last activity than the one kept, which finished after it, is not kept over it.
func (c *sessionReads) keep(key sessionKey, e sessionRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kept, ok := c.entries[key]; ok && kept.lastSeenNs > e.lastSeenNs {
		return
	}
	now := c.now()
	delete(c.entries, key)
	for k, kept := range c.entries {
		if now.Sub(kept.readAt) >= sessionReadTTL {
			delete(c.entries, k)
		}
	}
	for len(c.entries) >= maxSessionReads {
		var oldest sessionKey
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
