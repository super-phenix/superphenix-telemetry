package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPHonoursRemoteAddrByDefault(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "203.0.113.10:51234"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	got := clientIP(r, false)
	if got != "203.0.113.10" {
		t.Fatalf("clientIP without trust = %q, want %q (XFF must be ignored)", got, "203.0.113.10")
	}
}

func TestClientIPUsesXFFWhenTrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "10.0.0.1:51234"
	r.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	got := clientIP(r, true)
	if got != "203.0.113.10" {
		t.Fatalf("clientIP with trust = %q, want %q (left-most XFF)", got, "203.0.113.10")
	}
}

func TestClientIPFallsBackWhenXFFEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "10.0.0.1:51234"
	got := clientIP(r, true)
	if got != "10.0.0.1" {
		t.Fatalf("clientIP fallback = %q, want %q", got, "10.0.0.1")
	}
}

func TestClientIPHandlesMissingPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.RemoteAddr = "203.0.113.10"
	got := clientIP(r, false)
	if got != "203.0.113.10" {
		t.Fatalf("clientIP = %q, want %q", got, "203.0.113.10")
	}
}
