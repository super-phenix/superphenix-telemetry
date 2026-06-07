package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/super-phenix/superphenix-telemetry/internal/telemetry"
)

func TestRegistryExposesCounterToPrometheus(t *testing.T) {
	r := mustRegistry(t)
	ctx := context.Background()

	err := r.Record(ctx, telemetry.Metric{
		Name: "volumes_total", Kind: telemetry.KindCounter, Value: 5,
		Labels: map[string]string{"version": "0.4.0"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	body := scrape(t, r)
	if !strings.Contains(body, MetricPrefix+"volumes_total") {
		t.Fatalf("expected metric in scrape output, got:\n%s", body)
	}
	if !strings.Contains(body, `version="0.4.0"`) {
		t.Fatalf("expected version label in scrape output, got:\n%s", body)
	}
}

func TestRegistryExposesGaugeToPrometheus(t *testing.T) {
	r := mustRegistry(t)
	ctx := context.Background()
	if err := r.Record(ctx, telemetry.Metric{Name: "queue_depth", Kind: telemetry.KindGauge, Value: 42}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	body := scrape(t, r)
	if !strings.Contains(body, MetricPrefix+"queue_depth 42") {
		t.Fatalf("expected gauge=42 in scrape output, got:\n%s", body)
	}
}

func TestRegistryRejectsUnknownKind(t *testing.T) {
	r := mustRegistry(t)
	err := r.Record(context.Background(), telemetry.Metric{Name: "x", Kind: "histogram", Value: 1})
	if err == nil {
		t.Fatal("expected error for unsupported kind")
	}
}

func TestRegistryReusesInstrument(t *testing.T) {
	r := mustRegistry(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := r.Record(ctx, telemetry.Metric{Name: "events_total", Kind: telemetry.KindCounter, Value: 1}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	body := scrape(t, r)
	if !strings.Contains(body, MetricPrefix+"events_total 5") {
		t.Fatalf("counter should accumulate to 5, got:\n%s", body)
	}
}

func mustRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = r.Shutdown(context.Background()) })
	return r
}

func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	h := promhttp.HandlerFor(r.PrometheusRegistry(), promhttp.HandlerOpts{})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Body.String()
}
