package clickhouse

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/jmoiron/sqlx"
)

// eventsTTL reads the event archive's TTL from its table definition, which ClickHouse renders as toIntervalDay(<days>). It matches the
// whole clause, up to SETTINGS or the end: a hand-set TTL with a WHERE condition or a second rule renders more after the interval, and
// is not the retention the setting asks for.
var eventsTTL = regexp.MustCompile(`TTL ingested_date \+ toIntervalDay\((\d+)\)(?: SETTINGS |$)`)

// ApplyEventRetention sets the event archive's TTL to days when it holds another value, and reports whether it changed it (issue
// #1434). An unchanged retention issues no ALTER, so every server start does not rewrite the TTL. A shorter retention expires older
// events as ClickHouse applies the new TTL to the table's parts.
func ApplyEventRetention(ctx context.Context, db *sqlx.DB, days int) (bool, error) {
	var engine string
	if err := db.GetContext(ctx, &engine,
		"SELECT engine_full FROM system.tables WHERE database = currentDatabase() AND name = 'events'"); err != nil {
		return false, fmt.Errorf("read events table definition: %w", err)
	}
	if m := eventsTTL.FindStringSubmatch(engine); m != nil && m[1] == strconv.Itoa(days) {
		return false, nil
	}
	// days is an int the config validated, formatted here because a DDL statement takes no bound parameters.
	if _, err := db.ExecContext(ctx, "ALTER TABLE events MODIFY TTL ingested_date + INTERVAL "+strconv.Itoa(days)+" DAY"); err != nil {
		return false, fmt.Errorf("set events TTL: %w", err)
	}
	return true, nil
}

// ArchiveBytes is the space the archive's tables take on disk (issue #1434): the compressed size of their active parts. Parts a merge
// has replaced but not yet removed are left out, since ClickHouse removes them within minutes.
func (s *Store) ArchiveBytes(ctx context.Context) (int64, error) {
	var bytes int64
	if err := s.db.GetContext(ctx, &bytes,
		"SELECT toInt64(sum(bytes_on_disk)) FROM system.parts WHERE database = currentDatabase() AND active"); err != nil {
		return 0, fmt.Errorf("read archive size: %w", err)
	}
	return bytes, nil
}
