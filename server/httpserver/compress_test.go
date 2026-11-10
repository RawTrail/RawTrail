package httpserver

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec:server-rest-api/responses-are-compressed-for-clients-that-accept-it/a-large-response-is-compressed-when-the-client-accepts-it
// spec:server-rest-api/responses-are-compressed-for-clients-that-accept-it/a-client-that-accepts-no-encoding-gets-the-body-as-is
//
// TestBuild_CompressesResponses pins that the middleware chain compresses a response for a client that accepts gzip or zstd, and
// leaves it as is for a client that accepts neither or for a body too small to gain. A long agent session's Timeline was about
// 20 MB of uncompressed JSON on dogfood, the bulk of the tab's load time over a real network (issue #1550).
func TestBuild_CompressesResponses(t *testing.T) {
	t.Parallel()
	large := `{"turns":[` + strings.Repeat(`{"command":"go test ./...","args":["go","test","./..."]},`, 2000) + `{}]}`
	cases := []struct {
		name, acceptEncoding, body, wantEncoding string
	}{
		{name: "gzip accepted", acceptEncoding: "gzip", body: large, wantEncoding: "gzip"},
		{name: "zstd preferred over gzip", acceptEncoding: "gzip, deflate, br, zstd", body: large, wantEncoding: "zstd"},
		{name: "no encoding accepted", body: large},
		{name: "body too small to gain", acceptEncoding: "gzip", body: `{"ok":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/thing", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/thing", nil)
			if tc.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tc.acceptEncoding)
			}
			rr := httptest.NewRecorder()
			Build(mux, Options{Logger: newLogger(io.Discard)}).ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, tc.wantEncoding, rr.Header().Get("Content-Encoding"))
			assert.Equal(t, tc.body, decodeBody(t, tc.wantEncoding, rr.Body), "the client reads back exactly what the handler wrote")
			if tc.wantEncoding != "" {
				assert.Less(t, rr.Body.Len(), len(tc.body)/10, "a repetitive JSON body compresses at least tenfold")
				assert.Contains(t, rr.Header().Values("Vary"), "Accept-Encoding", "caches must key the response on the encoding")
			}
		})
	}
}

// decodeBody reverses the response's content encoding, if any.
func decodeBody(t *testing.T, encoding string, body io.Reader) string {
	t.Helper()
	r := body
	switch encoding {
	case "gzip":
		zr, err := gzip.NewReader(body)
		require.NoError(t, err)
		r = zr
	case "zstd":
		zr, err := zstd.NewReader(body)
		require.NoError(t, err)
		defer zr.Close()
		r = zr
	}
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}
