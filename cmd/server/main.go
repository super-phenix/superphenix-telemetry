package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/super-phenix/superphenix-telemetry/internal/anonymizer"
	"github.com/super-phenix/superphenix-telemetry/internal/logging"
	"github.com/super-phenix/superphenix-telemetry/internal/metrics"
	"github.com/super-phenix/superphenix-telemetry/internal/ratelimit"
	"github.com/super-phenix/superphenix-telemetry/internal/server"
)

type config struct {
	addr              string
	logLevel          string
	rateMax           int
	rateWindow        time.Duration
	trustForwardedFor bool
	readHeaderTimeout time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	shutdownTimeout   time.Duration
}

func loadConfig() config {
	return config{
		addr:              env("LISTEN_ADDR", ":8080"),
		logLevel:          env("LOG_LEVEL", "info"),
		rateMax:           envInt("RATE_LIMIT_MAX", 10),
		rateWindow:        envDuration("RATE_LIMIT_WINDOW", time.Hour),
		trustForwardedFor: envBool("TRUST_FORWARDED_FOR", false),
		readHeaderTimeout: envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		writeTimeout:      envDuration("WRITE_TIMEOUT", 10*time.Second),
		idleTimeout:       envDuration("IDLE_TIMEOUT", 60*time.Second),
		shutdownTimeout:   envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
	}
}

func main() {
	cfg := loadConfig()
	log := logging.New(cfg.logLevel)

	if err := run(cfg, log); err != nil {
		log.Error("server exited with error", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run(cfg config, log *slog.Logger) error {
	anon, err := anonymizer.New()
	if err != nil {
		return err
	}
	limiter := ratelimit.New(cfg.rateMax, cfg.rateWindow)
	defer limiter.Close()

	reg, err := metrics.New()
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
		defer cancel()
		if err := reg.Shutdown(ctx); err != nil {
			log.Warn("metrics shutdown", slog.String("err", err.Error()))
		}
	}()

	handler := server.New(server.Config{
		Logger:            log,
		Anon:              anon,
		Limiter:           limiter,
		Registry:          reg,
		TrustForwardedFor: cfg.trustForwardedFor,
	})

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.readHeaderTimeout,
		WriteTimeout:      cfg.writeTimeout,
		IdleTimeout:       cfg.idleTimeout,
	}

	log.Info("listening",
		slog.String("addr", cfg.addr),
		slog.Int("rate_max", cfg.rateMax),
		slog.Duration("rate_window", cfg.rateWindow),
		slog.Bool("trust_forwarded_for", cfg.trustForwardedFor),
	)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Info("shutdown requested", slog.String("signal", sig.String()))
	case err, ok := <-errCh:
		if ok && err != nil {
			return err
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.shutdownTimeout)
	defer cancel()
	return srv.Shutdown(ctx)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
