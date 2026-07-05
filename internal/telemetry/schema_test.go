package telemetry

import (
	"math"
	"strings"
	"testing"
)

func validReport() Report {
	return Report{
		SchemaVersion:  SchemaVersion,
		InstallationID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Metrics: []Metric{
			{Name: MetricAZCount, Kind: KindGauge, Value: 7, Labels: map[string]string{"region": "01234567", "az": "01234567"}},
		},
	}
}

func TestValidateAcceptsCleanReport(t *testing.T) {
	r := validReport()
	if err := r.Validate(); err != nil {
		t.Fatalf("clean report rejected: %v", err)
	}
}

func TestValidateRejectsWrongSchemaVersion(t *testing.T) {
	r := validReport()
	r.SchemaVersion = 999
	if err := r.Validate(); err == nil {
		t.Fatal("expected schema_version mismatch to be rejected")
	}
}

func TestValidateRejectsEmptyMetrics(t *testing.T) {
	r := validReport()
	r.Metrics = nil
	if err := r.Validate(); err == nil {
		t.Fatal("expected empty metrics to be rejected")
	}
}

func TestValidateRejectsTooManyMetrics(t *testing.T) {
	r := validReport()
	r.Metrics = make([]Metric, MaxMetricsPerReport+1)
	for i := range r.Metrics {
		r.Metrics[i] = Metric{Name: MetricRegionCount, Kind: KindGauge, Value: 1}
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected too-many-metrics to be rejected")
	}
}

func TestValidateRejectsBadName(t *testing.T) {
	bad := []string{
		"",                        // empty
		"Capital",                 // uppercase
		"1starts_with_digit",      // leading digit
		"has space",               // space
		"has-dash",                // dash
		strings.Repeat("a", 65),   // too long
		"x" + string([]byte{'/'}), // slash
	}
	for _, n := range bad {
		r := validReport()
		r.Metrics[0].Name = n
		if err := r.Validate(); err == nil {
			t.Fatalf("expected name %q to be rejected", n)
		}
	}
}

func TestValidateRejectsUnknownMetric(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = "unknown_metric"
	if err := r.Validate(); err == nil {
		t.Fatal("expected unknown metric name to be rejected")
	}
}

func TestValidateRejectsWrongKindForMetric(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricAZCount
	r.Metrics[0].Kind = KindCounter // AZCount is a gauge
	if err := r.Validate(); err == nil {
		t.Fatal("expected wrong kind for metric to be rejected")
	}
}

func TestValidateRejectsMissingRequiredLabels(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricOperatorInfo
	r.Metrics[0].Labels = nil // missing "version"
	if err := r.Validate(); err == nil {
		t.Fatal("expected missing required labels to be rejected")
	}
}

func TestValidateRejectsExtraLabels(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricAZCount
	r.Metrics[0].Labels = map[string]string{"region": "01234567", "az": "01234567", "extra": "value"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected extra labels to be rejected")
	}
}

func TestValidateRejectsInvalidLabelValues(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{MetricClusterInfo, map[string]string{"topology": "bad", "type": "storage", "version": "v1", "cluster": "01234567", "az": "01234567"}},
		{MetricClusterInfo, map[string]string{"topology": "hyperconverged", "type": "bad", "version": "v1", "cluster": "01234567", "az": "01234567"}},
	}
	for _, tt := range tests {
		r := validReport()
		r.Metrics[0].Name = tt.name
		r.Metrics[0].Labels = tt.labels
		if err := r.Validate(); err == nil {
			t.Errorf("expected invalid label values for %s to be rejected: %v", tt.name, tt.labels)
		}
	}
}

func TestValidateAcceptsValidComplexMetrics(t *testing.T) {
	r := Report{
		SchemaVersion:  SchemaVersion,
		InstallationID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Metrics: []Metric{
			{
				Name:  MetricClusterInfo,
				Kind:  KindGauge,
				Value: 1,
				Labels: map[string]string{
					"topology": "hyperconverged",
					"type":     "storage",
					"version":  "v1.2.3",
					"cluster":  "0123456789abcdef",
					"az":       "0123456789abcdef",
				},
			},
			{
				Name:  MetricComponentInfo,
				Kind:  KindGauge,
				Value: 1,
				Labels: map[string]string{
					"name":    "scheduler",
					"version": "v1.2.3",
					"cluster": "0123456789abcdef",
				},
			},
			{
				Name:   MetricNodeCount,
				Kind:   KindGauge,
				Value:  10,
				Labels: map[string]string{"cluster": "0123456789abcdef"},
			},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("valid complex report rejected: %v", err)
	}
}

func TestValidateRejectsNonFiniteValue(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		r := validReport()
		r.Metrics[0].Value = v
		if err := r.Validate(); err == nil {
			t.Fatalf("expected value %v to be rejected", v)
		}
	}
}

func TestValidateRejectsNegativeCounter(t *testing.T) {
	t.Skip("no allowed counters defined currently")
}

func TestValidateAllowsNegativeGauge(t *testing.T) {
	r := validReport()
	r.Metrics[0].Kind = KindGauge
	r.Metrics[0].Value = -1
	if err := r.Validate(); err != nil {
		t.Fatalf("gauges should allow negative values: %v", err)
	}
}

func TestValidateRejectsTooManyLabels(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricClusterInfo
	r.Metrics[0].Labels = map[string]string{
		"topology": "hyperconverged",
		"type":     "storage",
		"version":  "v1",
		"cluster":  "0123456789abcdef",
	}
	for i := 0; i < MaxLabelsPerMetric; i++ {
		r.Metrics[0].Labels["extra"+string(rune('a'+i))] = "v"
	}
	if err := r.Validate(); err == nil {
		t.Fatal("expected too-many-labels to be rejected")
	}
}

func TestValidateRejectsBadLabelKey(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricComponentInfo
	r.Metrics[0].Labels = map[string]string{"name": "n", "version": "v", "cluster": "0123456789abcdef", "BAD-Key": "v"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected bad label key to be rejected")
	}
}

func TestValidateRejectsBadLabelValue(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricComponentInfo
	r.Metrics[0].Labels = map[string]string{"name": "has space", "version": "v", "cluster": "0123456789abcdef"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected bad label value to be rejected")
	}
}

func TestValidateRejectsOversizedLabelValue(t *testing.T) {
	r := validReport()
	r.Metrics[0].Name = MetricComponentInfo
	r.Metrics[0].Labels = map[string]string{"name": strings.Repeat("a", MaxLabelValueLen+1), "version": "v", "cluster": "0123456789abcdef"}
	if err := r.Validate(); err == nil {
		t.Fatal("expected oversized label value to be rejected")
	}
}

func TestValidateAcceptsNewMetrics(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{MetricNodeCount, map[string]string{"cluster": "0123456789abcdef"}},
		{MetricRegionCount, nil},
	}
	for _, tt := range tests {
		r := Report{
			SchemaVersion:  SchemaVersion,
			InstallationID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Metrics: []Metric{
				{
					Name:   tt.name,
					Kind:   KindGauge,
					Value:  1,
					Labels: tt.labels,
				},
			},
		}
		if err := r.Validate(); err != nil {
			t.Errorf("new metric %s rejected: %v", tt.name, err)
		}
	}
}

func TestValidateAcceptsComponentInfoWithoutCluster(t *testing.T) {
	r := Report{
		SchemaVersion:  SchemaVersion,
		InstallationID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Metrics: []Metric{
			{
				Name:  MetricComponentInfo,
				Kind:  KindGauge,
				Value: 1,
				Labels: map[string]string{
					"name":    "scheduler",
					"version": "v1.2.3",
				},
			},
		},
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("component_info should allow optional cluster label: %v", err)
	}
}

func TestValidateRejectsNonAnonymizedIdentifier(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
	}{
		{MetricAZCount, map[string]string{"region": "us-east-1", "az": "01234567"}},
		{MetricAZCount, map[string]string{"region": "01234567", "az": "us-east-1a"}},
		{MetricNodeCount, map[string]string{"cluster": "cluster-a"}},
		{MetricClusterInfo, map[string]string{"topology": "hyperconverged", "type": "storage", "version": "v1", "cluster": "cluster-a", "az": "0123456789abcdef"}},
		{MetricClusterInfo, map[string]string{"topology": "hyperconverged", "type": "storage", "version": "v1", "cluster": "0123456789abcdef", "az": "us-east-1a"}},
	}
	for _, tt := range tests {
		r := validReport()
		r.Metrics[0].Name = tt.name
		r.Metrics[0].Labels = tt.labels
		if err := r.Validate(); err == nil {
			t.Errorf("expected non-anonymized label value for %s to be rejected: %v", tt.name, tt.labels)
		}
	}
}
