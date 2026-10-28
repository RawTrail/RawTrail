package operator

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	visibilityapi "github.com/rawtrail/rawtrail/server/visibility/api"
)

func getActivityPage(t *testing.T, reader *fakeActivityReader, path string) (int, map[string]json.RawMessage) {
	t.Helper()
	pv := uint32(200)
	reader.processes = map[int64]api.Process{2: {ID: 2, HostID: "h1", PID: 20, PIDVersion: &pv, Path: "/bin/zsh"}}
	srv := newActivityServer(t, fakeService{}, reader, allowAllAuthZ{})
	resp := doGet(t, srv, path)
	defer resp.Body.Close()
	var out map[string]json.RawMessage
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	}
	return resp.StatusCode, out
}

// spec:server-rest-api/agent-session-activity-pages/an-activity-list-pages-by-cursor
func TestActivityPageHandler(t *testing.T) {
	t.Parallel()

	t.Run("a class's page after the cursor, over the scope's processes", func(t *testing.T) {
		t.Parallel()
		reader := &fakeActivityReader{page: visibilityapi.ActivityPage{
			DNS: []visibilityapi.DNSActivityRow{{QueryName: "example.com"}}, NextCursor: "next",
		}}
		status, out := getActivityPage(t, reader, "/api/hosts/h1/activity/dns?scope=process&process=2&cursor=abc&limit=7&q=%20example%20")
		require.Equal(t, http.StatusOK, status)
		assert.JSONEq(t, `[{"query_name":"example.com","addresses":null,"pid":0,"pidversion":0,"count":0,"first_ns":0,"last_ns":0}]`,
			string(out["rows"]))
		assert.JSONEq(t, `"next"`, string(out["next_cursor"]))
		assert.Equal(t, "dns", reader.class)
		assert.Equal(t, "abc", reader.cursor)
		assert.Equal(t, 7, reader.filter.Limit)
		assert.Equal(t, "example", reader.filter.Search)
		assert.Equal(t, []visibilityapi.ProcessGeneration{{PID: 20, PIDVersion: 200}}, reader.filter.Generations)
	})

	t.Run("an empty page lists no rows and no next page", func(t *testing.T) {
		t.Parallel()
		for _, class := range []string{"files", "network", "dns"} {
			status, out := getActivityPage(t, &fakeActivityReader{}, "/api/hosts/h1/activity/"+class+"?scope=process&process=2")
			require.Equal(t, http.StatusOK, status, class)
			assert.JSONEq(t, `[]`, string(out["rows"]), class)
			assert.NotContains(t, out, "next_cursor", class)
		}
	})

	t.Run("the limit is bounded", func(t *testing.T) {
		t.Parallel()
		reader := &fakeActivityReader{}
		status, _ := getActivityPage(t, reader, "/api/hosts/h1/activity/files?scope=process&process=2&limit=100000")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, activityPageLimit, reader.filter.Limit)
	})

	for _, tc := range []struct {
		name   string
		path   string
		reader *fakeActivityReader
		want   int
	}{
		{"a malformed cursor", "/api/hosts/h1/activity/files?scope=process&process=2&cursor=x",
			&fakeActivityReader{err: visibilityapi.ErrInvalidActivityCursor}, http.StatusBadRequest},
		{"no scope", "/api/hosts/h1/activity/files", &fakeActivityReader{}, http.StatusBadRequest},
		// The failing scope read would answer 500, so a 400 shows the class was refused before the scope was read.
		{"an unknown class, refused before the scope is read", "/api/hosts/h1/activity/bogus?scope=process&process=2",
			&fakeActivityReader{processErr: errors.New("db")}, http.StatusBadRequest},
		{"a failed read", "/api/hosts/h1/activity/files?scope=process&process=2", &fakeActivityReader{err: errors.New("ch")},
			http.StatusInternalServerError},
		{"a failed scope", "/api/hosts/h1/activity/files?scope=process&process=2",
			&fakeActivityReader{processErr: errors.New("db")}, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			status, _ := getActivityPage(t, tc.reader, tc.path)
			assert.Equal(t, tc.want, status)
		})
	}

	t.Run("no reader", func(t *testing.T) {
		t.Parallel()
		srv := newActivityServer(t, fakeService{}, nil, allowAllAuthZ{})
		resp := doGet(t, srv, "/api/hosts/h1/activity/files?scope=process&process=2")
		defer resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})
}
