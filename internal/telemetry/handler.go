package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Recorder is anything that can ingest a single validated Metric. The
// handler depends on this interface rather than on *metrics.Registry so
// that tests can substitute a no-op or capturing recorder.
type Recorder interface {
	Record(ctx context.Context, m Metric) error
}

// Limiter is the subset of rate-limiter behaviour the handler needs.
type Limiter interface {
	Allow(key string) (allowed bool, retryAfter time.Duration)
}

// ClientIdentifier extracts an opaque, anonymised token for a request.
// The handler never sees a raw IP - it asks the caller via this function.
type ClientIdentifier func(*http.Request) string

// HandlerConfig collects the dependencies and tunables of the ingest
// endpoint. All fields are required except Logger, which defaults to a
// discard logger when nil, and MaxBody, which defaults to MaxBodyBytes.
type HandlerConfig struct {
	Recorder Recorder
	Limiter  Limiter
	ClientID ClientIdentifier
	Logger   *slog.Logger
	MaxBody  int64
}

// NewHandler returns an http.Handler that accepts a single Report on POST
// and records its metrics. Any other method returns 405. Bodies above
// MaxBody bytes are rejected before parsing.
func NewHandler(cfg HandlerConfig) http.Handler {
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = MaxBodyBytes
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			cfg.Logger.Warn("rejection: method not allowed", slog.String("method", r.Method))
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		token := cfg.ClientID(r)
		if ok, retry := cfg.Limiter.Allow(token); !ok {
			// Round up so Retry-After is never 0 when we're blocking.
			secs := int(retry.Seconds())
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			cfg.Logger.Info("rejection: rate limited", slog.String("retry_after", w.Header().Get("Retry-After")+"s"))
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBody)
		defer r.Body.Close()

		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		var report Report
		if err := dec.Decode(&report); err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				cfg.Logger.Warn("rejection: body too large", slog.Int64("limit", cfg.MaxBody))
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			cfg.Logger.Warn("rejection: invalid json", slog.String("err", err.Error()))
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		// Reject trailing junk - guarantees one report per request.
		if dec.More() {
			cfg.Logger.Warn("rejection: unexpected trailing data")
			http.Error(w, "unexpected trailing data", http.StatusBadRequest)
			return
		}

		if err := report.Validate(); err != nil {
			cfg.Logger.Warn("rejection: validation failed", slog.String("err", err.Error()))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		for i := range report.Metrics {
			m := report.Metrics[i]
			if m.Labels == nil {
				m.Labels = make(map[string]string)
			}
			if len(token) > 16 {
				m.Labels["hashed_ip"] = token[:16]
			} else {
				m.Labels["hashed_ip"] = token
			}
			if err := cfg.Recorder.Record(r.Context(), m); err != nil {
				cfg.Logger.Error("record metric", slog.String("name", m.Name), slog.String("err", err.Error()))
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
