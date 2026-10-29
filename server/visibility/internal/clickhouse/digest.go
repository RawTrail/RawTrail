package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// The digest's list sizes (issue #1429): a summary names the busiest directories and domains, not all of them.
const (
	digestDirectories = 20
	digestDomains     = 50
)

// Digest SQL fragments. A file row's directory is its path up to the last slash, the root for a file directly under it, and a rolled-up
// row's own path. A domain is the registrable domain of a lookup's name or of the host a connection named, lowercased and without
// a trailing dot, or the whole name when it has none (localhost).
const (
	digestPath      = "JSONExtractString(payload, 'path')"
	digestDirectory = "if(JSONExtractBool(payload, 'rolled_up'), " + digestPath + ", substring(" + digestPath + ", 1, greatest(minus(length(" +
		digestPath + "), position(reverse(" + digestPath + "), '/')), 1)))"
	digestName = "lower(replaceRegexpOne(if(event_type = 'dns_query', JSONExtractString(payload, 'query_name'), " +
		"JSONExtractString(payload, 'remote_hostname')), '\\\\.$', ''))"
	digestDomain = "if(cutToFirstSignificantSubdomain(name) = '', name, cutToFirstSignificantSubdomain(name))"
)

// ActivityDigest reads the filter's scope as a summary describes it: its busiest directories, its paths outside the project, its
// busiest domains and which of them are new to the host, and its events over time. The reads run in parallel over one scope.
func (s *Store) ActivityDigest(ctx context.Context, filter api.DigestFilter) (api.ActivityDigest, error) {
	out := api.ActivityDigest{
		Directories: []api.DirectoryActivity{}, Domains: []api.DomainActivity{}, Timeline: make([]int64, filter.Buckets),
	}
	if len(filter.Generations) == 0 && len(filter.Spans) == 0 {
		return out, nil
	}
	scope, args := activityScope(filter.ActivityFilter)
	ctx = withActivityQuerySize(ctx)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		out.Directories, out.DirectoriesTruncated, err = s.digestDirectories(gctx, scope, args)
		return err
	})
	if filter.Project != "" {
		g.Go(func() (err error) {
			out.OutsideProject, err = s.digestOutsideProject(gctx, scope, args, filter.Project)
			return err
		})
	}
	g.Go(func() (err error) {
		out.Domains, out.DomainsTruncated, err = s.digestDomains(gctx, scope, args, filter.HostID, filter.StartNs)
		return err
	})
	g.Go(func() (err error) {
		out.Timeline, err = s.digestTimeline(gctx, scope, args, filter)
		return err
	})
	if err := g.Wait(); err != nil {
		return api.ActivityDigest{}, err
	}
	return out, nil
}

// digestDirectories groups the scope's file events by directory, most events first.
func (s *Store) digestDirectories(ctx context.Context, scope string, args []any) ([]api.DirectoryActivity, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT directory, paths, operations.1, operations.2
		FROM (
			SELECT `+digestDirectory+` AS directory, toInt64(uniqExact(`+digestPath+`)) AS paths,
				sumMap(([`+activityFileOperation+`], [toInt64(`+activityWeight+`)])) AS operations,
				toInt64(sum(`+activityWeight+`)) AS weight
			FROM events FINAL
			WHERE event_type IN `+activityFileTypes+` AND `+scope+`
			GROUP BY directory
			ORDER BY weight DESC, directory
			LIMIT ?
		)`, append(args, digestDirectories+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse digest directories: %w", err)
	}
	type directoryRow struct {
		api.DirectoryActivity
		operations []string
		counts     []int64
	}
	read, truncated, err := scanActivity(rows, digestDirectories, func(r *directoryRow) []any {
		return []any{&r.Directory, &r.Paths, &r.operations, &r.counts}
	})
	if err != nil {
		return nil, false, err
	}
	out := make([]api.DirectoryActivity, len(read))
	for i, r := range read {
		out[i] = r.DirectoryActivity
		out[i].ByOperation = make(map[string]int64, len(r.operations))
		for j, operation := range r.operations {
			out[i].ByOperation[operation] = r.counts[j]
		}
	}
	return out, truncated, nil
}

// digestOutsideProject counts the distinct paths the scope's file events touched that are neither the project nor under it.
func (s *Store) digestOutsideProject(ctx context.Context, scope string, args []any, project string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT toInt64(uniqExactIf(`+digestPath+`, `+digestPath+` != ? AND NOT startsWith(`+digestPath+`, ?)))
		FROM events FINAL
		WHERE event_type IN `+activityFileTypes+` AND `+scope,
		append([]any{strings.TrimSuffix(project, "/"), strings.TrimSuffix(project, "/") + "/"}, args...)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("clickhouse digest outside project: %w", err)
	}
	return n, nil
}

// digestDomains groups the scope's lookups and named connections by domain, most first, and marks the domains the host had not looked
// up before startNs.
func (s *Store) digestDomains(ctx context.Context, scope string, args []any, hostID string, startNs int64) ([]api.DomainActivity, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT domain, toInt64(sumIf(weight, event_type = 'dns_query')) AS lookups,
			toInt64(sumIf(weight, event_type = 'network_connect')) AS connections
		FROM (
			SELECT event_type, `+digestName+` AS name, `+digestDomain+` AS domain, `+activityWeight+` AS weight
			FROM events FINAL
			WHERE `+flowTypesPredicate+` AND `+scope+`
		)
		WHERE domain != ''
		GROUP BY domain
		ORDER BY lookups + connections DESC, domain
		LIMIT ?`, append(args, digestDomains+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse digest domains: %w", err)
	}
	domains, truncated, err := scanActivity(rows, digestDomains, func(r *api.DomainActivity) []any {
		return []any{&r.Domain, &r.Lookups, &r.Connections}
	})
	if err != nil || len(domains) == 0 {
		return []api.DomainActivity{}, truncated, err
	}
	seen, err := s.domainsSeenBefore(ctx, hostID, startNs, domains)
	if err != nil {
		return nil, false, err
	}
	for i := range domains {
		domains[i].New = !seen[domains[i].Domain]
	}
	return domains, truncated, nil
}

// domainsSeenBefore reports which of the domains the host looked up before startNs, in the history the archive retains.
func (s *Store) domainsSeenBefore(ctx context.Context, hostID string, startNs int64, domains []api.DomainActivity) (map[string]bool, error) {
	marks := make([]string, len(domains))
	args := []any{hostID, startNs}
	for i, d := range domains {
		marks[i] = "?"
		args = append(args, d.Domain)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT domain
		FROM (
			SELECT `+digestName+` AS name, `+digestDomain+` AS domain
			FROM events
			WHERE `+hostPredicate+` AND event_type = 'dns_query' AND timestamp_ns < ?
		)
		WHERE domain IN (`+strings.Join(marks, ", ")+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse digest domains seen: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, fmt.Errorf("scan digest domain seen: %w", err)
		}
		seen[domain] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest domains seen: %w", err)
	}
	return seen, nil
}

// digestTimeline counts the scope's events in each of the filter's buckets. Each bucket is one nanosecond wider than the span divided
// by Buckets, so the span's last moment falls in the last bucket; an event outside the span is not counted.
func (s *Store) digestTimeline(ctx context.Context, scope string, args []any, filter api.DigestFilter) ([]int64, error) {
	width := (filter.EndNs-filter.StartNs)/int64(filter.Buckets) + 1
	rows, err := s.db.QueryContext(ctx, `
		SELECT toInt64(intDiv(minus(timestamp_ns, ?), ?)) AS bucket, toInt64(sum(`+activityWeight+`))
		FROM events FINAL
		WHERE event_type IN `+activityAllTypes+` AND `+scope+` AND timestamp_ns >= ? AND timestamp_ns <= ?
		GROUP BY bucket`, append(append([]any{filter.StartNs, width}, args...), filter.StartNs, filter.EndNs)...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse digest timeline: %w", err)
	}
	defer rows.Close()
	timeline := make([]int64, filter.Buckets)
	for rows.Next() {
		var bucket, n int64
		if err := rows.Scan(&bucket, &n); err != nil {
			return nil, fmt.Errorf("scan digest timeline: %w", err)
		}
		timeline[bucket] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest timeline: %w", err)
	}
	return timeline, nil
}

// AgentPromptCount counts one session's prompts, as AgentPrompts lists them with no search.
func (s *Store) AgentPromptCount(ctx context.Context, hostID, sessionID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT toInt64(count())
		FROM events FINAL
		WHERE `+hostPredicate+` AND event_type = 'agent_prompt' AND JSONExtractString(payload, 'agent_session_id') = ?`,
		hostID, sessionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("clickhouse agent prompt count: %w", err)
	}
	return n, nil
}
