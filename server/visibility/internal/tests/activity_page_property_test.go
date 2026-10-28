//go:build integration

package tests

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

// Reading an activity class page by page, at any page size, lists the same rows in the same order as one read that holds them all,
// with or without a search (issue #1447). Events are drawn from a few artifacts, two processes and three instants, so rows tie on
// their first-seen time and the order falls through to the rest of the key at page boundaries.
//
// spec:server-rest-api/agent-session-activity-pages/an-activity-list-pages-by-cursor
func TestEventArchive_ActivityPagesMatchOneRead(t *testing.T) {
	t.Parallel()
	arch := openTestArchive(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	gens := []visibilityapi.ProcessGeneration{{PID: 300, PIDVersion: 700}, {PID: 301, PIDVersion: 699}}
	paths := []string{"/r/a", "/r/b", "/r/ab"}
	addresses := []string{"10.0.0.1", "10.0.0.2"}
	names := []string{"a.example", "b.example", "ab.example"}
	iter := 0
	rapid.Check(t, func(rt *rapid.T) {
		iter++
		host := "activity-pbt-" + strconv.Itoa(iter)
		n := rapid.IntRange(1, 30).Draw(rt, "n")
		var events []visibilityapi.Event
		// Each class's rows, keyed by what groups a row and holding the artifact a search matches, are worked out here from what was
		// drawn: they anchor the one read that the pages are compared to.
		drawn := map[string]map[string]string{
			visibilityapi.ActivityFiles: {}, visibilityapi.ActivityNetwork: {}, visibilityapi.ActivityDNS: {},
		}
		for i := range n {
			is := strconv.Itoa(i)
			gen := gens[rapid.IntRange(0, 1).Draw(rt, "gen"+is)]
			at := base + int64(rapid.IntRange(0, 2).Draw(rt, "at"+is))*int64(time.Second)
			proc := `"pid":` + strconv.FormatInt(gen.PID, 10) + `,"pidversion":` + strconv.FormatInt(gen.PIDVersion, 10)
			switch rapid.IntRange(0, 2).Draw(rt, "class"+is) {
			case 0:
				op := rapid.SampledFrom([]string{"modify", "create", "delete"}).Draw(rt, "op"+is)
				path := rapid.SampledFrom(paths).Draw(rt, "path"+is)
				drawn[visibilityapi.ActivityFiles][path+"|"+op+"|"+strconv.FormatInt(gen.PID, 10)] = path
				events = append(events, activityEvent("f"+is, host, "file_activity", at,
					`{`+proc+`,"operation":"`+op+`","path":"`+path+`"}`))
			case 1:
				address := rapid.SampledFrom(addresses).Draw(rt, "address"+is)
				port := rapid.SampledFrom([]string{"443", "80"}).Draw(rt, "port"+is)
				drawn[visibilityapi.ActivityNetwork][address+"|"+port+"|"+strconv.FormatInt(gen.PID, 10)] = address
				events = append(events, activityEvent("n"+is, host, "network_connect", at, `{`+proc+
					`,"protocol":"tcp","direction":"outbound","remote_address":"`+address+`","remote_port":`+port+`}`))
			default:
				name := rapid.SampledFrom(names).Draw(rt, "name"+is)
				drawn[visibilityapi.ActivityDNS][name+"|"+strconv.FormatInt(gen.PID, 10)] = name
				events = append(events, activityEvent("d"+is, host, "dns_query", at,
					`{`+proc+`,"query_name":"`+name+`","query_type":"A"}`))
			}
		}
		require.NoError(rt, arch.Insert(ctx, events))
		// Each search matches some artifacts of every class it can name: "A" paths and names, "b." names, "0.1" one address.
		search := rapid.SampledFrom([]string{"", "A", "b.", "0.1"}).Draw(rt, "search")
		pageSize := rapid.IntRange(1, 4).Draw(rt, "pageSize")
		filter := visibilityapi.ActivityFilter{HostID: host, Generations: gens, Search: search, Limit: 1000}

		// read lists a class's rows, whole, and the key that groups each.
		read := func(class string, limit int) ([]any, []string) {
			paged := filter
			paged.Limit = limit
			var rows []any
			var keys []string
			cursor := ""
			for range n + 1 {
				page, err := arch.ActivityPage(ctx, paged, class, cursor)
				require.NoError(rt, err)
				for _, f := range page.Files {
					rows, keys = append(rows, f), append(keys, f.Path+"|"+f.Operation+"|"+strconv.FormatInt(f.PID, 10))
				}
				for _, c := range page.Network {
					rows = append(rows, c)
					keys = append(keys, c.RemoteAddress+"|"+strconv.FormatInt(c.RemotePort, 10)+"|"+strconv.FormatInt(c.PID, 10))
				}
				for _, d := range page.DNS {
					rows, keys = append(rows, d), append(keys, d.QueryName+"|"+strconv.FormatInt(d.PID, 10))
				}
				if cursor = page.NextCursor; cursor == "" {
					return rows, keys
				}
			}
			rt.Fatalf("%s: the cursors did not end within %d pages", class, n+1)
			return nil, nil
		}
		for _, class := range []string{visibilityapi.ActivityFiles, visibilityapi.ActivityNetwork, visibilityapi.ActivityDNS} {
			var want []string
			for key, artifact := range drawn[class] {
				if strings.Contains(strings.ToLower(artifact), strings.ToLower(search)) {
					want = append(want, key)
				}
			}
			whole, keys := read(class, 1000)
			require.ElementsMatch(rt, want, keys, "%s: the one read lists each drawn row the search keeps, once", class)
			paged, _ := read(class, pageSize)
			require.Equal(rt, whole, paged, class)
		}
	})
}
