package clickhouse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/rawtrail/rawtrail/server/visibility/api"
)

// Each activity class's order (issue #1424): first seen, then every column that groups a row, so the order is total and a page can
// continue from its last row. The names are the list queries' column aliases.
const (
	fileOrder    = "first, path, operation, source_path, rolled_up, pid, pv"
	networkOrder = "first, address, port, protocol, pid, pv"
	dnsOrder     = "first, name, pid, pv"
)

// activitySearch is what each class's search matches: the artifact a row names, case-insensitively.
var activitySearch = map[string]string{
	api.ActivityFiles: "(positionCaseInsensitiveUTF8(JSONExtractString(payload, 'path'), ?) > 0 OR " +
		"positionCaseInsensitiveUTF8(JSONExtractString(payload, 'source_path'), ?) > 0)",
	api.ActivityNetwork: "(positionCaseInsensitiveUTF8(JSONExtractString(payload, 'remote_address'), ?) > 0 OR " +
		"positionCaseInsensitiveUTF8(JSONExtractString(payload, 'remote_hostname'), ?) > 0)",
	api.ActivityDNS: "positionCaseInsensitiveUTF8(JSONExtractString(payload, 'query_name'), ?) > 0",
}

// keyset is a list row's place in its class's order, the values of the order's columns, or nil for the start of the list.
type keyset []any

// having keeps the rows after the keyset's position, or every row for the start.
func (k keyset) having(order string) string {
	if k == nil {
		return ""
	}
	return "HAVING (" + order + ") > (" + strings.TrimSuffix(strings.Repeat("?, ", len(k)), ", ") + ")"
}

// args appends the keyset's values and the limit to the scope's arguments, copying them so the shared scope's are not written.
func (k keyset) args(scope []any, limit int) []any {
	out := make([]any, 0, len(scope)+len(k)+1)
	return append(append(append(out, scope...), k...), limit)
}

// activityCursor is a list row's position as a cursor carries it: its class, so a cursor of one class cannot read another, and the
// values of its order's columns.
type activityCursor struct {
	Class    string `json:"c"`
	First    int64  `json:"t"`
	Text     string `json:"a,omitempty"` // path, address or name
	Op       string `json:"o,omitempty"` // operation or protocol
	Source   string `json:"s,omitempty"`
	RolledUp bool   `json:"r,omitempty"`
	Port     int64  `json:"n,omitempty"`
	PID      int64  `json:"p"`
	PV       int64  `json:"v"`
}

func encodeActivityCursor(c activityCursor) string {
	raw, err := json.Marshal(c)
	if err != nil {
		panic(err) // unreachable: a struct of strings, integers and a bool always marshals
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func fileCursor(r api.FileActivityRow) string {
	return encodeActivityCursor(activityCursor{Class: api.ActivityFiles, First: r.FirstNs, Text: r.Path, Op: r.Operation,
		Source: r.SourcePath, RolledUp: r.RolledUp, PID: r.PID, PV: r.PIDVersion})
}

func networkCursor(r api.NetworkActivityRow) string {
	return encodeActivityCursor(activityCursor{Class: api.ActivityNetwork, First: r.FirstNs, Text: r.RemoteAddress, Port: r.RemotePort,
		Op: r.Protocol, PID: r.PID, PV: r.PIDVersion})
}

func dnsCursor(r api.DNSActivityRow) string {
	return encodeActivityCursor(activityCursor{Class: api.ActivityDNS, First: r.FirstNs, Text: r.QueryName, PID: r.PID, PV: r.PIDVersion})
}

// nextCursor is the cursor of the page after a list: its last row's, when the list was cut, and none otherwise.
func nextCursor[R any](rows []R, cut bool, cursor func(R) string) string {
	if !cut || len(rows) == 0 {
		return ""
	}
	return cursor(rows[len(rows)-1])
}

// decodeActivityCursor reads a cursor of the class as the keyset of its order's columns. An empty cursor is the start of the list.
func decodeActivityCursor(class, token string) (keyset, error) {
	if token == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: not base64url: %w", api.ErrInvalidActivityCursor, err)
	}
	var c activityCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%w: not an activity cursor: %w", api.ErrInvalidActivityCursor, err)
	}
	// Every listed row was first seen at a positive time; a cursor without one, or of another class, names no row of this list.
	if c.Class != class || c.First <= 0 {
		return nil, fmt.Errorf("%w: not a %s cursor", api.ErrInvalidActivityCursor, class)
	}
	switch class {
	case api.ActivityFiles:
		return keyset{c.First, c.Text, c.Op, c.Source, c.RolledUp, c.PID, c.PV}, nil
	case api.ActivityNetwork:
		return keyset{c.First, c.Text, c.Port, c.Op, c.PID, c.PV}, nil
	default:
		return keyset{c.First, c.Text, c.PID, c.PV}, nil
	}
}

// ActivityPage lists one class's rows after a cursor, filter.Limit of them, in the rollup's order, with the cursor of the next page.
func (s *Store) ActivityPage(ctx context.Context, filter api.ActivityFilter, class, cursor string) (api.ActivityPage, error) {
	if class != api.ActivityFiles && class != api.ActivityNetwork && class != api.ActivityDNS {
		return api.ActivityPage{}, fmt.Errorf("%w: no class %q", api.ErrInvalidActivityCursor, class)
	}
	after, err := decodeActivityCursor(class, cursor)
	if err != nil {
		return api.ActivityPage{}, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultActivityLimit
	}
	var page api.ActivityPage
	if len(filter.Generations) == 0 && len(filter.Spans) == 0 {
		return page, nil
	}
	scope, args := activityScope(filter)
	if filter.Search != "" {
		clause := activitySearch[class]
		scope += " AND " + clause
		args = append(slices.Clip(args), slices.Repeat([]any{filter.Search}, strings.Count(clause, "?"))...)
	}
	ctx = withActivityQuerySize(ctx)
	var cut bool
	switch class {
	case api.ActivityFiles:
		page.Files, cut, err = s.fileActivity(ctx, scope, args, after, limit)
		page.NextCursor = nextCursor(page.Files, cut, fileCursor)
	case api.ActivityNetwork:
		page.Network, cut, err = s.networkActivity(ctx, scope, args, after, limit)
		page.NextCursor = nextCursor(page.Network, cut, networkCursor)
	default:
		page.DNS, cut, err = s.dnsActivity(ctx, scope, args, after, limit)
		page.NextCursor = nextCursor(page.DNS, cut, dnsCursor)
	}
	if err != nil {
		return api.ActivityPage{}, err
	}
	return page, nil
}
