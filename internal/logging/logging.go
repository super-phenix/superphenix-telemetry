package logging

import (
	"log/slog"
	"net/http"
	"os"
	"time"
)

// New returns a JSON slog logger writing to stderr at the given level.
func New(levelString string) *slog.Logger {
	var level slog.Level

	switch levelString {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// statusRecorder captures the response status so the access log can
// report it without breaking the ResponseWriter contract.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// AccessLog wraps h and emits one log line per request. The clientToken
// function turns the request into a short anonymous identifier; the
// middleware itself is unaware of the underlying IP.
func AccessLog(log *slog.Logger, clientToken func(*http.Request) string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		log.Info("request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
			slog.String("client", clientToken(r)),
		)
	})
}
