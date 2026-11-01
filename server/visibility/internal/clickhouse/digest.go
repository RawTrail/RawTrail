package clickhouse

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/errgroup"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// The digest's list sizes (issue #1429): a summary names the busiest directories and domains, not all of them. Domains are folded
// from the busiest digestNames names.
const (
	digestDirectories = 20
	digestDomains     = 50
	digestNames       = 10_000
)

// Digest SQL fragments. A file row's directory is its path up to the last slash, the root for a file directly under it, and a rolled-up
// row's own path. A name is a lookup's name or the host a connection named, lowercased and without a trailing dot.
const (
	digestPath      = "JSONExtractString(payload, 'path')"
	digestDirectory = "if(JSONExtractBool(payload, 'rolled_up'), " + digestPath + ", substring(" + digestPath + ", 1, greatest(minus(length(" +
		digestPath + "), position(reverse(" + digestPath + "), '/')), 1)))"
	digestName = "lower(replaceRegexpOne(if(event_type = 'dns_query', JSONExtractString(payload, 'query_name'), " +
		"JSONExtractString(payload, 'remote_hostname')), '\\\\.$', ''))"
)

// ActivityDigest reads the filter's scope as a summary describes it: its busiest directories, its paths outside the project, its
// busiest domains and which of them are new to the host, and its events over time. The reads run in parallel over one scope.
func (s *Store) ActivityDigest(ctx context.Context, filter api.DigestFilter) (api.ActivityDigest, error) {
	out := api.ActivityDigest{
		Directories: []api.DirectoryActivity{}, Domains: []api.DomainActivity{}, Timeline: make([]int64, filter.Buckets),
		TimelineStartNs: filter.StartNs, TimelineBucketNs: (filter.EndNs-filter.StartNs)/int64(filter.Buckets) + 1,
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
	g.Go(func() (err error) {
		out.Files, err = s.digestFiles(gctx, scope, args, filter.Project)
		return err
	})
	g.Go(func() (err error) {
		out.Domains, out.DomainsTruncated, err = s.digestDomains(gctx, scope, args, filter.HostID, filter.StartNs)
		return err
	})
	g.Go(func() (err error) {
		out.Timeline, out.TimelineStartNs, out.TimelineBucketNs, err = s.digestTimeline(gctx, scope, args, filter)
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

// agentStatePath matches the files Claude Code keeps for itself, which every session writes whatever it was asked to do: its settings,
// transcripts and caches under the home folder, and under /tmp its commands' output, the shell's working-directory note and its
// sockets. A path ES reports is resolved, so /tmp is /private/tmp.
const agentStatePath = `^(/Users/[^/]+/(\.claude(/|\.json($|\.)|$)|\.local/(state|share)/claude(/|$)|Library/Caches/claude-cli-nodejs(/|$))` +
	`|/private/tmp/(claude-[0-9]+(/|$)|claude-[0-9a-f]+-cwd$|cc-socks(/|$)))`

// digestFileOperations are the operations that change a file: an open reads one and a connect reaches a socket.
const digestFileOperations = "['create', 'modify', 'rename', 'delete', 'file_rename', 'file_truncate', 'file_delete']"

// digestFiles counts the paths the scope changed, as a summary reads them. Claude Code's own files are counted apart and nowhere else.
// A temporary file, created and then, last of all, deleted or renamed away in the scope as an atomic write's is, is counted apart too:
// a file deleted and created again is a change. A rolled-up row stands for many files under a directory, so it is never temporary.
// The rest are the files changed, and those outside the project are counted again; with no project, none are outside it. A path only
// read or connected to is counted nowhere.
func (s *Store) digestFiles(ctx context.Context, scope string, args []any, project string) (api.FileDigest, error) {
	root := strings.TrimSuffix(project, "/")
	var out api.FileDigest
	query := `
		SELECT toInt64(countIf(own AND changed)), toInt64(countIf(NOT own AND changed AND NOT temporary)),
			toInt64(countIf(NOT own AND changed AND NOT temporary AND ? != '' AND path != ? AND NOT startsWith(path, ?))),
			toInt64(countIf(NOT own AND temporary))
		FROM (
			SELECT path, match(path, ?) AS own, changes > 0 AS changed, NOT rolled AND created > 0 AND removed > created AS temporary
			FROM (
				SELECT path, countIf(has(` + digestFileOperations + `, operation)) AS changes, max(rolled_up) AS rolled,
					maxIf(timestamp_ns, operation = 'create') AS created,
					maxIf(timestamp_ns, operation IN ('delete', 'file_delete', 'moved_away')) AS removed
				FROM (
					SELECT ` + digestPath + ` AS path, ` + activityFileOperation + ` AS operation, timestamp_ns,
						JSONExtractBool(payload, 'rolled_up') AS rolled_up
					FROM events FINAL
					WHERE event_type IN ` + activityFileTypes + ` AND ` + scope + `
					UNION ALL
					SELECT JSONExtractString(payload, 'source_path') AS path, 'moved_away' AS operation, timestamp_ns,
						JSONExtractBool(payload, 'rolled_up') AS rolled_up
					FROM events FINAL
					WHERE event_type IN ` + activityFileTypes + ` AND JSONExtractString(payload, 'source_path') != '' AND ` + scope + `
				)
				GROUP BY path
			)
		)`
	queryArgs := append([]any{root, root, root + "/", agentStatePath}, args...)
	err := s.db.QueryRowContext(ctx, query, append(queryArgs, args...)...).Scan(&out.AgentFiles, &out.Changed, &out.OutsideProject,
		&out.Temporary)
	if err != nil {
		return api.FileDigest{}, fmt.Errorf("clickhouse digest files: %w", err)
	}
	return out, nil
}

// digestDomains groups the scope's lookups and named connections by registrable domain, most first, and marks the domains the host had
// not looked up before startNs. The names are read busiest first, up to digestNames of them, and folded by the public suffix list.
func (s *Store) digestDomains(ctx context.Context, scope string, args []any, hostID string, startNs int64) ([]api.DomainActivity, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, toInt64(sumIf(weight, event_type = 'dns_query')) AS lookups,
			toInt64(sumIf(weight, event_type = 'network_connect')) AS connections
		FROM (
			SELECT event_type, `+digestName+` AS name, `+activityWeight+` AS weight
			FROM events FINAL
			WHERE `+flowTypesPredicate+` AND `+scope+`
		)
		WHERE name != ''
		GROUP BY name
		ORDER BY lookups + connections DESC, name
		LIMIT ?`, append(args, digestNames+1)...)
	if err != nil {
		return nil, false, fmt.Errorf("clickhouse digest domains: %w", err)
	}
	names, cut, err := scanActivity(rows, digestNames, func(r *api.DomainActivity) []any {
		return []any{&r.Domain, &r.Lookups, &r.Connections}
	})
	if err != nil {
		return nil, false, err
	}
	domains, truncated := foldDomains(names)
	if len(domains) == 0 {
		return domains, cut || truncated, nil
	}
	seen, err := s.domainsSeenBefore(ctx, hostID, startNs, domains)
	if err != nil {
		return nil, false, err
	}
	for i := range domains {
		domains[i].New = !seen[domains[i].Domain]
	}
	return domains, cut || truncated, nil
}

// foldDomains sums the names' lookups and connections by registrable domain, most first and then by domain, and keeps the busiest
// digestDomains of them, reporting whether there were more.
func foldDomains(names []api.DomainActivity) ([]api.DomainActivity, bool) {
	index := map[string]int{}
	out := []api.DomainActivity{}
	for _, n := range names {
		domain := registrableDomain(n.Domain)
		i, ok := index[domain]
		if !ok {
			i = len(out)
			index[domain] = i
			out = append(out, api.DomainActivity{Domain: domain})
		}
		out[i].Lookups += n.Lookups
		out[i].Connections += n.Connections
	}
	slices.SortFunc(out, func(a, b api.DomainActivity) int {
		return cmp.Or(cmp.Compare(b.Lookups+b.Connections, a.Lookups+a.Connections), cmp.Compare(a.Domain, b.Domain))
	})
	return out[:min(len(out), digestDomains)], len(out) > digestDomains
}

// registrableDomain is the domain a name belongs to by the public suffix list, private suffixes included (api.github.com is
// github.com), or the whole name when it has none: an IP address a connection named, a single label such as localhost, or a public
// suffix itself. The list treats an IP address as its own suffix.
func registrableDomain(name string) string {
	if domain, err := publicsuffix.EffectiveTLDPlusOne(name); err == nil {
		return domain
	}
	return name
}

// domainsSeenBefore reports which of the domains the host looked up before startNs, in the history the archive retains: a name is
// read when it is a domain or under one, and counts for the domain it belongs to by the public suffix list.
func (s *Store) domainsSeenBefore(ctx context.Context, hostID string, startNs int64, domains []api.DomainActivity) (map[string]bool, error) {
	arms := make([]string, len(domains))
	args := []any{hostID, startNs}
	for i, d := range domains {
		arms[i] = "name = ? OR endsWith(name, ?)"
		args = append(args, d.Domain, "."+d.Domain)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT name
		FROM (
			SELECT `+digestName+` AS name
			FROM events
			WHERE `+hostPredicate+` AND event_type = 'dns_query' AND timestamp_ns < ?
		)
		WHERE `+strings.Join(arms, " OR "), args...)
	if err != nil {
		return nil, fmt.Errorf("clickhouse digest domains seen: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan digest domain seen: %w", err)
		}
		seen[registrableDomain(name)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate digest domains seen: %w", err)
	}
	return seen, nil
}

// digestTimeline counts the scope's events in equal buckets of a span: the filter's, widened to the scope's first and last event so
// none falls outside it. Each bucket is one nanosecond wider than the span divided by Buckets, so the span's last moment falls in the
// last bucket. An event recorded between the two reads falls after the span and is left out. It returns the counts and the span's
// start and bucket width.
func (s *Store) digestTimeline(ctx context.Context, scope string, args []any, filter api.DigestFilter) ([]int64, int64, int64, error) {
	var first, last, events int64
	err := s.db.QueryRowContext(ctx, `
		SELECT toInt64(min(timestamp_ns)), toInt64(max(timestamp_ns)), toInt64(count())
		FROM events FINAL
		WHERE event_type IN `+activityAllTypes+` AND `+scope, args...).Scan(&first, &last, &events)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("clickhouse digest timeline span: %w", err)
	}
	start, end := filter.StartNs, filter.EndNs
	if events > 0 {
		start, end = min(start, first), max(end, last)
	}
	width := (end-start)/int64(filter.Buckets) + 1
	rows, err := s.db.QueryContext(ctx, `
		SELECT toInt64(intDiv(minus(timestamp_ns, ?), ?)) AS bucket, toInt64(sum(`+activityWeight+`))
		FROM events FINAL
		WHERE event_type IN `+activityAllTypes+` AND `+scope+` AND timestamp_ns BETWEEN ? AND ?
		GROUP BY bucket`, append(append([]any{start, width}, args...), start, end)...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("clickhouse digest timeline: %w", err)
	}
	defer rows.Close()
	timeline := make([]int64, filter.Buckets)
	for rows.Next() {
		var bucket, n int64
		if err := rows.Scan(&bucket, &n); err != nil {
			return nil, 0, 0, fmt.Errorf("scan digest timeline: %w", err)
		}
		timeline[bucket] = n
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, fmt.Errorf("iterate digest timeline: %w", err)
	}
	return timeline, start, width, nil
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
