package bootstrap

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/rawtrail/rawtrail/server/config"
	"github.com/rawtrail/rawtrail/server/httpserver"
)

// HTTPServerOptions is what NewHTTPServer needs beyond the config: the binary's name, its routes, and an optional request recorder.
type HTTPServerOptions struct {
	ServiceName string
	Handler     http.Handler
	// Metrics receives one ObserveHTTPRequest call per request. nil disables it.
	Metrics httpserver.HTTPRequestRecorder
}

// NewHTTPServer builds the http.Server both HTTP binaries serve: the shared middleware chain, client-address resolution from
// EDR_TRUSTED_PROXIES, the shared timeouts, and TLS unless EDR_TLS_TERMINATED_BY_PROXY is set. It returns an error for an invalid
// proxy list or an unloadable keypair, before anything is served.
func NewHTTPServer(ctx context.Context, cfg *config.Config, logger *slog.Logger, opts HTTPServerOptions) (*http.Server, error) {
	// The resolver is built only when proxies are configured. httpserver.Build skips its middleware on nil, and httpserver.ClientIP
	// falls back to the peer address, which is what an empty list would resolve to anyway, so the default deployment saves a hop.
	var clientIPResolver *httpserver.ClientIPResolver
	if len(cfg.TrustedProxies) > 0 {
		var err error
		clientIPResolver, err = httpserver.NewClientIPResolver(cfg.TrustedProxies)
		if err != nil {
			logger.ErrorContext(ctx, "EDR_TRUSTED_PROXIES invalid", "err", err)
			return nil, err
		}
		logger.InfoContext(ctx, "trusting X-Forwarded-For from proxies", "trusted", cfg.TrustedProxies)
	}
	handler := httpserver.Build(opts.Handler, httpserver.Options{
		Logger:      logger,
		ServiceName: opts.ServiceName,
		// HSTS keys on the client-facing scheme: emit it when a front proxy terminates TLS too, since the client reaches us over
		// HTTPS. Safe under the accidental-flag case because browsers ignore HSTS received over a direct plain-HTTP hit.
		TLSEnabled:       cfg.ExternalTLS(),
		ClientIPResolver: clientIPResolver,
		Metrics:          opts.Metrics,
	})
	srv := httpserver.NewServer(cfg.ListenAddr, handler)
	if cfg.TLSTerminatedByProxy {
		// Leave srv.TLSConfig nil so RunAndShutdown serves plaintext HTTP. Config validation already guaranteed no cert files are set
		// in this mode. Warn loudly so an operator who set the flag by accident (no proxy actually in front) sees that the data plane
		// is plaintext on the bind address.
		logger.WarnContext(ctx, "serving plaintext HTTP: EDR_TLS_TERMINATED_BY_PROXY=1 is set; this is only safe behind a "+
			"TLS-terminating proxy (PaaS edge, ALB, nginx). Do not expose this bind address directly to agents or the internet.",
			"listen_addr", cfg.ListenAddr)
		return srv, nil
	}
	if err := httpserver.ConfigureTLS(ctx, srv, httpserver.TLSOptions{
		CertFile: cfg.TLSCertFile,
		KeyFile:  cfg.TLSKeyFile,
		Logger:   logger,
	}); err != nil {
		return nil, err
	}
	return srv, nil
}
