package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRecorder struct {
	mu      sync.Mutex
	calls   []Metric
	failOn  string
	failErr error
}

func (f *fakeRecorder) Record(_ context.Context, m Metric) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != "" && m.Name == f.failOn {
		return f.failErr
	}
	f.calls = append(f.calls, m)
	return nil
}

func (f *fakeRecorder) Calls() []Metric {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Metric, len(f.calls))
	copy(out, f.calls)
	return out
}

type fakeLimiter struct {
	allow bool
	retry time.Duration
}

func (f *fakeLimiter) Allow(string) (bool, time.Duration) {
	return f.allow, f.retry
}

func newHandler(rec Recorder, lim Limiter) http.Handler {
	return NewHandler(HandlerConfig{
		Recorder: rec,
		Limiter:  lim,
		ClientID: func(*http.Request) string { return "test-client" },
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func doPost(h http.Handler, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/telemetry", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestHandlerAcceptsValidReport(t *testing.T) {
	rec := &fakeRecorder{}
	h := newHandler(rec, &fakeLimiter{allow: true})

	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics: []Metric{
			{Name: MetricAZCount, Kind: KindGauge, Value: 3},
			{Name: MetricOperatorInfo, Kind: KindGauge, Value: 1, Labels: map[string]string{"version": "1.0"}},
		},
	})
	w := doPost(h, body)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	if got := rec.Calls(); len(got) != 2 {
		t.Fatalf("recorded %d metrics, want 2", len(got))
	}
}

func TestHandlerRejectsWrongMethod(t *testing.T) {
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	req := httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
	if w.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("missing Allow header on 405")
	}
}

func TestHandlerThrottles(t *testing.T) {
	rec := &fakeRecorder{}
	h := newHandler(rec, &fakeLimiter{allow: false, retry: 42 * time.Second})

	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: MetricAZCount, Kind: KindGauge, Value: 1}},
	})
	w := doPost(h, body)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "42" {
		t.Fatalf("Retry-After = %q, want 42", got)
	}
	if len(rec.Calls()) != 0 {
		t.Fatalf("throttled request must not have been recorded")
	}
}

func TestHandlerRetryAfterRoundsUpFromZero(t *testing.T) {
	// retry < 1s should still produce Retry-After: 1
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: false, retry: 100 * time.Millisecond})
	w := doPost(h, mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: MetricAZCount, Kind: KindGauge, Value: 1}},
	}))
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestHandlerRejectsInvalidJSON(t *testing.T) {
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	w := doPost(h, []byte("{not json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	w := doPost(h, []byte(`{"schema_version":1,"metrics":[],"extra":"nope"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown field", w.Code)
	}
}

func TestHandlerRejectsValidationFailure(t *testing.T) {
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: "Bad-Name", Kind: KindCounter, Value: 1}},
	})
	w := doPost(h, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid name", w.Code)
	}
}

func TestHandlerRejectsTooLargeBody(t *testing.T) {
	h := NewHandler(HandlerConfig{
		Recorder: &fakeRecorder{},
		Limiter:  &fakeLimiter{allow: true},
		ClientID: func(*http.Request) string { return "c" },
		MaxBody:  16, // tiny
	})
	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: MetricAZCount, Kind: KindGauge, Value: 1}},
	})
	w := doPost(h, body)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", w.Code)
	}
}

func TestHandlerRejectsTrailingData(t *testing.T) {
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: MetricAZCount, Kind: KindGauge, Value: 1}},
	})
	body = append(body, []byte(`{"another":"object"}`)...)
	w := doPost(h, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for trailing data", w.Code)
	}
}

func TestHandlerReturns500OnRecorderFailure(t *testing.T) {
	rec := &fakeRecorder{failOn: MetricAZCount, failErr: errors.New("boom")}
	h := newHandler(rec, &fakeLimiter{allow: true})
	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics:       []Metric{{Name: MetricAZCount, Kind: KindGauge, Value: 1}},
	})
	w := doPost(h, body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestHandlerAddsHashedIP(t *testing.T) {
	rec := &fakeRecorder{}
	h := NewHandler(HandlerConfig{
		Recorder: rec,
		Limiter:  &fakeLimiter{allow: true},
		ClientID: func(*http.Request) string { return "hashed-ip-token" },
	})

	body := mustJSON(t, Report{
		SchemaVersion: SchemaVersion,
		Metrics: []Metric{
			{Name: MetricAZCount, Kind: KindGauge, Value: 1},
		},
	})
	w := doPost(h, body)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if got := calls[0].Labels["hashed_ip"]; got != "hashed-ip-token" {
		t.Errorf("hashed_ip label = %q, want %q", got, "hashed-ip-token")
	}
}

func TestHandlerErrorBodiesAreSafe(t *testing.T) {
	// Make sure we don't reflect anything from the request body in error
	// responses - that would defeat anonymisation.
	h := newHandler(&fakeRecorder{}, &fakeLimiter{allow: true})
	w := doPost(h, []byte(`{"schema_version":1,"metrics":[{"name":"secret-host-name","kind":"counter","value":1}]}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-host-name") {
		t.Fatal("error body leaked client-supplied value back to caller")
	}
}
