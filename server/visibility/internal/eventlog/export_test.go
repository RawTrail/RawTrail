package eventlog

import "context"

// CountPendingUpTo exposes countPendingUpTo to the external test package, which can import the schema testkit without a cycle.
func (s *Store) CountPendingUpTo(ctx context.Context, limit int64) (int64, error) {
	return s.countPendingUpTo(ctx, limit)
}
