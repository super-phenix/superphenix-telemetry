// Package server composes the HTTP routes (ingest, /metrics, /healthz,
// /readyz) and the request-level middleware (rate limiting, access log).
package server

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/super-phenix/superphenix-telemetry/internal/anonymizer"
	"github.com/super-phenix/superphenix-telemetry/internal/logging"
	"github.com/super-phenix/superphenix-telemetry/internal/metrics"
	"github.com/super-phenix/superphenix-telemetry/internal/ratelimit"
	"github.com/super-phenix/superphenix-telemetry/internal/telemetry"
)

// Config holds runtime configuration for the HTTP server.
//
// TrustForwardedFor controls whether the X-Forwarded-For header is
// honoured when extracting the client IP. Set this only when the server
// sits behind a proxy you control; otherwise any client can spoof the
// header to evade rate limiting.
type Config struct {
	Logger            *slog.Logger
	Anon              *anonymizer.Anonymizer
	Limiter           *ratelimit.Limiter
	Registry          *metrics.Registry
	TrustForwardedFor bool
}

// New returns an http.Handler that wires up the public routes.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()

	clientID := func(r *http.Request) string {
		return cfg.Anon.Hash(clientIP(r, cfg.TrustForwardedFor))
	}
	shortClient := func(r *http.Request) string {
		return cfg.Anon.ShortHash(clientIP(r, cfg.TrustForwardedFor))
	}

	mux.Handle("/v1/telemetry", telemetry.NewHandler(telemetry.HandlerConfig{
		Recorder: cfg.Registry,
		Limiter:  cfg.Limiter,
		ClientID: clientID,
		Logger:   cfg.Logger,
	}))

	mux.Handle("/metrics", promhttp.HandlerFor(cfg.Registry.PrometheusRegistry(), promhttp.HandlerOpts{
		// Disable per-scrape logging from promhttp; we already log via slog.
		ErrorLog: nil,
	}))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return logging.AccessLog(cfg.Logger, shortClient, mux)
}

// clientIP extracts the address used for rate limiting. When
// trustForwardedFor is true we read the left-most entry of
// X-Forwarded-For (the original client per the de-facto convention).
// Otherwise we use the connecting socket's RemoteAddr, which is the only
// trustworthy source absent a known reverse proxy.
func clientIP(r *http.Request, trustForwardedFor bool) string {
	if trustForwardedFor {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if comma := strings.IndexByte(xff, ','); comma >= 0 {
				xff = xff[:comma]
			}
			xff = strings.TrimSpace(xff)
			if xff != "" {
				return xff
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
