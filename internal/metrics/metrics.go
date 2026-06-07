// Package metrics maintains the OpenTelemetry meters that translate
// incoming reports into Prometheus time series.
//
// Counters and gauges from the wire are recorded via OTel instruments;
// the Prometheus exporter publishes them through promhttp on the
// /metrics endpoint. The set of distinct instruments is cached so we
// pay the construction cost once per (name, kind) pair, not per request.
package metrics

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/super-phenix/superphenix-telemetry/internal/telemetry"
)

// MetricPrefix is prepended to every name received from clients before
// it is exposed on /metrics. It both namespaces our output and prevents
// collision with the server's own self-metrics (process_*, go_*, ...).
const MetricPrefix = "superphenix_telemetry_"

// Registry owns the OTel meter provider, the Prometheus registry, and
// the instrument cache. It is safe for concurrent use.
type Registry struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
	promReg  *prometheus.Registry

	mu       sync.Mutex
	counters map[string]metric.Float64Counter
	gauges   map[string]metric.Float64Gauge
}

// New builds a Registry whose instruments are exported through a fresh
// Prometheus registry. Pass the registry to promhttp.HandlerFor to expose
// /metrics.
func New() (*Registry, error) {
	promReg := prometheus.NewRegistry()
	// WithoutScopeInfo / WithoutTargetInfo drop the otel_scope_* labels and
	// the target_info gauge - neither is useful here and they bloat every
	// time series with the same noise.
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(promReg),
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	return &Registry{
		provider: provider,
		meter:    provider.Meter("github.com/super-phenix/superphenix-telemetry"),
		promReg:  promReg,
		counters: make(map[string]metric.Float64Counter),
		gauges:   make(map[string]metric.Float64Gauge),
	}, nil
}

// PrometheusRegistry returns the underlying Prometheus registry so the
// HTTP handler can expose it.
func (r *Registry) PrometheusRegistry() *prometheus.Registry { return r.promReg }

// Shutdown flushes the meter provider. Safe to call once at process exit.
func (r *Registry) Shutdown(ctx context.Context) error {
	return r.provider.Shutdown(ctx)
}

// Record observes a single metric, lazily constructing the OTel
// instrument on first sight.
func (r *Registry) Record(ctx context.Context, m telemetry.Metric) error {
	attrs := attrsFor(m.Labels)
	switch m.Kind {
	case telemetry.KindCounter:
		c, err := r.counter(m.Name)
		if err != nil {
			return err
		}
		c.Add(ctx, m.Value, metric.WithAttributes(attrs...))
	case telemetry.KindGauge:
		g, err := r.gauge(m.Name)
		if err != nil {
			return err
		}
		g.Record(ctx, m.Value, metric.WithAttributes(attrs...))
	default:
		return fmt.Errorf("unsupported kind %q", m.Kind)
	}
	return nil
}

func (r *Registry) counter(name string) (metric.Float64Counter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c, nil
	}
	c, err := r.meter.Float64Counter(MetricPrefix + name)
	if err != nil {
		return nil, fmt.Errorf("counter %s: %w", name, err)
	}
	r.counters[name] = c
	return c, nil
}

func (r *Registry) gauge(name string) (metric.Float64Gauge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g, nil
	}
	g, err := r.meter.Float64Gauge(MetricPrefix + name)
	if err != nil {
		return nil, fmt.Errorf("gauge %s: %w", name, err)
	}
	r.gauges[name] = g
	return g, nil
}

func attrsFor(labels map[string]string) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		out = append(out, attribute.String(k, v))
	}
	return out
}
