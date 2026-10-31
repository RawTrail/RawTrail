package operator

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rawtrail/rawtrail/server/detection/api"
	identityapi "github.com/rawtrail/rawtrail/server/identity/api"
)

type fakeStoreSizes struct {
	sizes api.StoreSizes
	err   error
}

func (f fakeStoreSizes) StoreSizes(context.Context) (api.StoreSizes, error) { return f.sizes, f.err }

func storageServer(t *testing.T, az identityapi.AuthZ, configure func(*Handler)) *httptest.Server {
	t.Helper()
	h := New(fakeService{}, az, slog.Default())
	configure(h)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// spec:server-configuration/the-store-size-is-reported/the-store-size-and-retention-are-reported
// spec:server-configuration/the-store-size-is-reported/a-store-past-the-threshold-is-flagged
func TestStorage_ReportsSizesRetentionAndTheWarning(t *testing.T) {
	t.Parallel()
	sizes := api.StoreSizes{EventArchiveBytes: 80_000_000_000, DatabaseBytes: 30_000_000_000}
	for _, tc := range []struct {
		name     string
		warnGB   int
		wantWarn int64
		wantOver bool
	}{
		{"past the threshold", 100, 100_000_000_000, true},
		{"under the threshold", 200, 200_000_000_000, false},
		{"a threshold of 0 never warns", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := storageServer(t, allowAllAuthZ{}, func(h *Handler) { h.SetStorage(fakeStoreSizes{sizes: sizes}, 7, tc.warnGB) })
			resp := doGet(t, srv, "/api/v1/admin/storage")
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			var body storageResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, storageResponse{
				EventArchiveBytes: 80_000_000_000, DatabaseBytes: 30_000_000_000, TotalBytes: 110_000_000_000,
				EventRetentionDays: 7, WarnBytes: tc.wantWarn, OverWarn: tc.wantOver,
			}, body)
		})
	}
}

func TestStorage_RefusalsAndFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		az        identityapi.AuthZ
		configure func(*Handler)
		want      int
	}{
		{"needs detection config read", denyAllAuthZ{}, func(h *Handler) { h.SetStorage(fakeStoreSizes{}, 30, 100) }, http.StatusForbidden},
		{"a failed read is a server error", allowAllAuthZ{},
			func(h *Handler) { h.SetStorage(fakeStoreSizes{err: errors.New("clickhouse down")}, 30, 100) }, http.StatusInternalServerError},
		{"not wired", allowAllAuthZ{}, func(*Handler) {}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := doGet(t, storageServer(t, tc.az, tc.configure), "/api/v1/admin/storage")
			defer resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}
