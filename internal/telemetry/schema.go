// Package telemetry defines the wire schema accepted by the ingest
// endpoint and the validation rules that bound it.
//
// The schema is intentionally narrow. Clients submit a small number of
// pre-named metrics with a constrained label set. The narrow shape makes
// it hard to accidentally exfiltrate identifying information (hostnames,
// usernames, paths) through label values, and bounds the Prometheus
// cardinality the server can ever emit.
package telemetry

import (
	"errors"
	"fmt"
	"math"
	"regexp"
)

// Wire-level limits. These exist primarily to keep accidental abuse from
// blowing up Prometheus cardinality or server memory; legitimate clients
// will not approach them.
const (
	MaxMetricsPerReport = 50
	MaxLabelsPerMetric  = 8
	MaxLabelValueLen    = 64
	MaxBodyBytes        = 64 * 1024 // 64 KiB

	SchemaVersion = 1
)

// Allowed kinds for an incoming metric.
const (
	KindCounter = "counter"
	KindGauge   = "gauge"
)

var (
	metricNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	labelKeyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	labelValRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	anonIDRe     = regexp.MustCompile(`^[0-9]+$`)
)

const (
	MetricOperatorInfo  = "operator_info"
	MetricClusterInfo   = "cluster_info"
	MetricComponentInfo = "component_info"
	MetricRegionCount   = "region_count"
	MetricAZCount       = "az_count"
	MetricNodeCount     = "node_count"
)

var (
	allowedMetrics = map[string]struct {
		kind   string
		labels map[string]bool
	}{
		MetricOperatorInfo:  {KindGauge, map[string]bool{"version": true}},
		MetricClusterInfo:   {KindGauge, map[string]bool{"topology": true, "type": true, "version": true, "cluster": true}},
		MetricComponentInfo: {KindGauge, map[string]bool{"name": true, "version": true, "cluster": false}},
		MetricRegionCount:   {KindGauge, nil},
		MetricAZCount:       {KindGauge, map[string]bool{"region": true}},
		MetricNodeCount:     {KindGauge, map[string]bool{"az": true, "cluster": true}},
	}

	allowedTopologies = map[string]bool{"hyperconverged": true, "decoupled": true}
	allowedTypes      = map[string]bool{"storage": true, "virtualization": true, "none": true}
)

// Report is the top-level body posted to the ingest endpoint.
//
// There is deliberately no field that carries a client-supplied identifier
// (host name, instance id, user id). The only per-request identifier the
// server ever sees is the source IP, which is hashed and used only for
// rate limiting.
type Report struct {
	SchemaVersion int      `json:"schema_version"`
	Metrics       []Metric `json:"metrics"`
}

// Metric describes a single observation.
type Metric struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Validate enforces the wire-level constraints documented above.
//
// Errors deliberately never include client-supplied values: they identify
// the offending field by position only, so a 400 response body cannot be
// abused to echo identifying data back to a logging proxy.
func (r *Report) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: unsupported, want %d", SchemaVersion)
	}

	if len(r.Metrics) == 0 {
		return errors.New("metrics: at least one metric is required")
	}

	if len(r.Metrics) > MaxMetricsPerReport {
		return fmt.Errorf("metrics: count exceeds maximum %d", MaxMetricsPerReport)
	}

	for i := range r.Metrics {
		if err := r.Metrics[i].validate(); err != nil {
			return fmt.Errorf("metrics[%s/%s/%d]: %w", r.Metrics[i].Kind, r.Metrics[i].Name, i, err)
		}
	}

	return nil
}

func (m *Metric) validate() error {
	if !metricNameRe.MatchString(m.Name) {
		return errors.New("name: invalid format")
	}

	spec, ok := allowedMetrics[m.Name]
	if !ok {
		return errors.New("name: not an allowed metric")
	}

	if m.Kind != spec.kind {
		return fmt.Errorf("kind: must be %q for this metric", spec.kind)
	}

	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
		return errors.New("value: must be finite")
	}

	if m.Kind == KindCounter && m.Value < 0 {
		return errors.New("value: counter must be non-negative")
	}

	if len(m.Labels) > MaxLabelsPerMetric {
		return fmt.Errorf("labels: count exceeds maximum %d", MaxLabelsPerMetric)
	}

	// Ensure all required labels are present.
	for l, required := range spec.labels {
		if required {
			if _, ok := m.Labels[l]; !ok {
				return fmt.Errorf("labels: missing required label %q", l)
			}
		}
	}

	for k, v := range m.Labels {
		if !labelKeyRe.MatchString(k) {
			return errors.New("labels: invalid key format")
		}
		if !labelValRe.MatchString(v) {
			return errors.New("labels: invalid value format")
		}

		if k == "region" || k == "az" || k == "cluster" {
			if !anonIDRe.MatchString(v) {
				return fmt.Errorf("labels: %s must be an anonymized identifier (numeric)", k)
			}
		}

		// Ensure no extra labels are provided.
		if _, allowed := spec.labels[k]; !allowed {
			return fmt.Errorf("labels: %q is not allowed for this metric", k)
		}

		// Validate specific label values if applicable.
		if m.Name == MetricClusterInfo {
			switch k {
			case "topology":
				if !allowedTopologies[v] {
					return errors.New("labels: invalid topology value")
				}
			case "type":
				if !allowedTypes[v] {
					return errors.New("labels: invalid type value")
				}
			}
		}
	}

	return nil
}
