package server

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/super-phenix/superphenix-telemetry/internal/anonymizer"
	"github.com/super-phenix/superphenix-telemetry/internal/metrics"
	"github.com/super-phenix/superphenix-telemetry/internal/ratelimit"
)

// TestEndToEnd exercises the full pipeline: ingest -> validation ->
// recorder -> Prometheus scrape. It is the contract test for the public
// surface of the service.
func TestEndToEnd(t *testing.T) {
	anon, err := anonymizer.NewWithSalt(bytes32())
	if err != nil {
		t.Fatalf("anonymizer: %v", err)
	}
	limiter := ratelimit.New(10, time.Hour)
	defer limiter.Close()
	reg, err := metrics.New()
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}

	handler := New(Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Anon:     anon,
		Limiter:  limiter,
		Registry: reg,
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Submit a report with new metrics.
	body := `{"schema_version":1,"metrics":[
		{"name":"az_count","kind":"gauge","value":3,"labels":{"region":"1"}},
		{"name":"node_count","kind":"gauge","value":5,"labels":{"az":"2","cluster":"1"}},
		{"name":"region_count","kind":"gauge","value":2},
		{"name":"cluster_info","kind":"gauge","value":1,"labels":{"topology":"hyperconverged","type":"storage","version":"v1.2.3","cluster":"3"}}
	]}`
	resp, err := http.Post(srv.URL+"/v1/telemetry", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		got, _ := io.ReadAll(resp.Body)
		t.Fatalf("post status = %d, body = %s", resp.StatusCode, got)
	}

	// Scrape /metrics and confirm the values are exposed.
	mresp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	defer mresp.Body.Close()
	mb, _ := io.ReadAll(mresp.Body)
	mbs := string(mb)

	for _, m := range []struct {
		name  string
		value string
	}{
		{"superphenix_telemetry_az_count", "3"},
		{"superphenix_telemetry_node_count", "5"},
		{"superphenix_telemetry_region_count", "2"},
		{"superphenix_telemetry_cluster_info", "1"},
	} {
		if !strings.Contains(mbs, m.name) || !strings.Contains(mbs, `hashed_ip="`) {
			t.Fatalf("expected metric %s with hashed_ip in scrape, got:\n%s", m.name, mbs)
		}
		if !strings.Contains(mbs, `} `+m.value) {
			t.Fatalf("expected value %s for %s in scrape, got:\n%s", m.value, m.name, mbs)
		}
	}

	// Verify hashed_ip length is 16.
	// We look for the first occurrence and extract the value.
	const needle = `hashed_ip="`
	idx := strings.Index(mbs, needle)
	if idx == -1 {
		t.Fatal("hashed_ip not found in metrics")
	}
	val := mbs[idx+len(needle):]
	end := strings.IndexByte(val, '"')
	if end == -1 {
		t.Fatal("malformed metrics output")
	}
	hashedIP := val[:end]
	if len(hashedIP) != 16 {
		t.Fatalf("hashed_ip length = %d, want 16 (value: %s)", len(hashedIP), hashedIP)
	}

	// Healthz and readyz must respond.
	for _, p := range []string{"/healthz", "/readyz"} {
		hr, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		hr.Body.Close()
		if hr.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", p, hr.StatusCode)
		}
	}
}

func TestEndToEndRateLimited(t *testing.T) {
	anon, _ := anonymizer.NewWithSalt(bytes32())
	limiter := ratelimit.New(2, time.Hour)
	defer limiter.Close()
	reg, _ := metrics.New()

	handler := New(Config{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Anon:     anon,
		Limiter:  limiter,
		Registry: reg,
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	body := `{"schema_version":1,"metrics":[{"name":"az_count","kind":"gauge","value":1,"labels":{"region":"1"}}]}`
	for i := 0; i < 2; i++ {
		resp, err := http.Post(srv.URL+"/v1/telemetry", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("post %d status = %d", i, resp.StatusCode)
		}
	}
	// Third request from the same client must be throttled.
	resp, err := http.Post(srv.URL+"/v1/telemetry", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post 3: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("third post status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After on 429 response")
	}
}

func bytes32() []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(i + 1)
	}
	return b
}
