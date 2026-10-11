package bootstrap

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fleetdm/edr/server/config"
	"github.com/fleetdm/edr/server/httpserver"
)

// spec:server-availability/ingest-drains-and-resolves-clients-like-the-server/a-forwarded-client-address-is-believed-only-from-a-trusted-proxy
//
// TestNewHTTPServer_ClientAddress pins the X-Forwarded-For handling both HTTP binaries get from NewHTTPServer (issue #1248): a
// forwarded address is believed only when the peer is a configured trusted proxy. Believing it from anyone would let any client
// choose the address that the access log, spans and per-IP rate limits see.
func TestNewHTTPServer_ClientAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		trustedProxies []string
		peer           string
		want           string
	}{
		{"trusted proxy forwards the client address", []string{"10.0.0.0/8"}, "10.1.2.3:5555", "203.0.113.9"},
		{"untrusted peer is its own address", []string{"10.0.0.0/8"}, "198.51.100.7:5555", "198.51.100.7"},
		{"no trusted proxies ignores the header", nil, "10.1.2.3:5555", "10.1.2.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Test-Client-Ip", httpserver.ClientIP(r))
			})
			cfg := &config.Config{ListenAddr: "127.0.0.1:0", TLSTerminatedByProxy: true, TrustedProxies: tc.trustedProxies}
			srv, err := NewHTTPServer(t.Context(), cfg, discardLogger(), HTTPServerOptions{ServiceName: "test", Handler: echo})
			require.NoError(t, err)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", "203.0.113.9")
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)

			assert.Equal(t, tc.want, rec.Header().Get("X-Test-Client-Ip"))
		})
	}
}

// TestNewHTTPServer_Setup pins the rest of what NewHTTPServer decides: HSTS whenever the client-facing scheme is HTTPS, including
// behind a TLS-terminating proxy, and a refusal to build from an invalid proxy list or a missing keypair.
func TestNewHTTPServer_Setup(t *testing.T) {
	t.Parallel()
	okHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	t.Run("HSTS behind a TLS-terminating proxy", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{ListenAddr: "127.0.0.1:0", TLSTerminatedByProxy: true}
		srv, err := NewHTTPServer(t.Context(), cfg, discardLogger(), HTTPServerOptions{ServiceName: "test", Handler: okHandler})
		require.NoError(t, err)
		assert.Nil(t, srv.TLSConfig, "the proxy terminates TLS, so this listener serves plaintext")
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
	})

	t.Run("an invalid proxy list is refused", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{ListenAddr: "127.0.0.1:0", TLSTerminatedByProxy: true, TrustedProxies: []string{"not-a-cidr"}}
		_, err := NewHTTPServer(t.Context(), cfg, discardLogger(), HTTPServerOptions{ServiceName: "test", Handler: okHandler})
		require.Error(t, err)
	})

	t.Run("a missing keypair is refused", func(t *testing.T) {
		t.Parallel()
		cfg := &config.Config{ListenAddr: "127.0.0.1:0", TLSCertFile: "/nonexistent/cert.pem", TLSKeyFile: "/nonexistent/key.pem"}
		_, err := NewHTTPServer(t.Context(), cfg, discardLogger(), HTTPServerOptions{ServiceName: "test", Handler: okHandler})
		require.Error(t, err)
	})
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
