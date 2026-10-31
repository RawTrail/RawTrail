package mysql

import (
	"context"
	"fmt"

	"github.com/rawtrail/rawtrail/server/detection/api"
)

// StoreSizes reads how much disk the event archive and this database take (issue #1434). The database's figure is its tables' data and
// indexes as information_schema reports them, which InnoDB estimates and refreshes as statistics are recalculated, so it is close
// rather than exact.
func (s *Store) StoreSizes(ctx context.Context) (api.StoreSizes, error) {
	archive, err := s.archive.ArchiveBytes(ctx)
	if err != nil {
		return api.StoreSizes{}, err
	}
	var database int64
	if err := s.db.GetContext(ctx, &database, `
		SELECT CAST(COALESCE(SUM(data_length + index_length), 0) AS SIGNED)
		FROM information_schema.tables WHERE table_schema = DATABASE()`); err != nil {
		return api.StoreSizes{}, fmt.Errorf("read database size: %w", err)
	}
	return api.StoreSizes{EventArchiveBytes: archive, DatabaseBytes: database}, nil
}
