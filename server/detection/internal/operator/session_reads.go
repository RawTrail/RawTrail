package operator

import (
	"context"
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
	// flight lets concurrent misses on one session, a session view's tabs opening together, share one read.
	flight singleflight.Group
	now    func() time.Time
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
func (c *sessionReads) get(
	ctx context.Context, session *api.AgentSessionSummary, read func(context.Context) (api.ProcessTreeResult, error),
) (api.ProcessTreeResult, error) {
	key := sessionKey{hostID: session.HostID, sessionID: session.AgentSessionID}
	if tree, ok := c.kept(key, session.LastSeenNs); ok {
		return tree, nil
	}
	// The shared read outlives a caller that gives up, so the others waiting on it are not failed by that caller's cancel.
	shared := context.WithoutCancel(ctx)
	v, err, _ := c.flight.Do(key.hostID+"\x00"+key.sessionID, func() (any, error) {
		tree, err := read(shared)
		if err != nil {
			return api.ProcessTreeResult{}, err
		}
		c.keep(key, sessionRead{tree: tree, lastSeenNs: session.LastSeenNs, readAt: c.now()})
		return tree, nil
	})
	if err != nil {
		return api.ProcessTreeResult{}, err
	}
	return v.(api.ProcessTreeResult), nil
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

// keep stores a read, dropping the expired ones and then, while the cache is full, the oldest.
func (c *sessionReads) keep(key sessionKey, e sessionRead) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
