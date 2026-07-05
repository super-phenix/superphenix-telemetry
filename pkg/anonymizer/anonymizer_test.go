package anonymizer

import (
	"strings"
	"testing"
)

func TestNewProducesStaticSalt(t *testing.T) {
	salt := "test-salt"
	a, err := New(salt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New(salt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Hash("1.2.3.4") != b.Hash("1.2.3.4") {
		t.Fatal("two independently created anonymizers with the same salt produced different hashes")
	}
}

func TestHashStableWithinInstance(t *testing.T) {
	a := mustAnonymizer(t)
	first := a.Hash("203.0.113.7")
	for i := 0; i < 10; i++ {
		if got := a.Hash("203.0.113.7"); got != first {
			t.Fatalf("hash changed on iteration %d: got %s want %s", i, got, first)
		}
	}
}

func TestHashPortStripped(t *testing.T) {
	a := mustAnonymizer(t)
	if a.Hash("203.0.113.7:8080") != a.Hash("203.0.113.7") {
		t.Fatal("expected port to be stripped before hashing")
	}
}

func TestHashIPv6Bracketed(t *testing.T) {
	a := mustAnonymizer(t)
	if a.Hash("[2001:db8::1]:443") != a.Hash("2001:db8::1") {
		t.Fatal("expected bracketed IPv6 address to canonicalise to bare form")
	}
}

func TestShortHashPrefixOfFull(t *testing.T) {
	a := mustAnonymizer(t)
	full := a.Hash("198.51.100.42")
	short := a.ShortHash("198.51.100.42")
	if !strings.HasPrefix(full, short) {
		t.Fatalf("short hash %s is not a prefix of full hash %s", short, full)
	}
	if len(short) != 8 {
		t.Fatalf("short hash length = %d, want 8", len(short))
	}
}

func TestHashRejectsCorrelationAcrossSalts(t *testing.T) {
	a, err := NewWithSalt(bytesOfLen(32, 0x01))
	if err != nil {
		t.Fatalf("NewWithSalt: %v", err)
	}
	b, err := NewWithSalt(bytesOfLen(32, 0x02))
	if err != nil {
		t.Fatalf("NewWithSalt: %v", err)
	}
	if a.Hash("10.0.0.1") == b.Hash("10.0.0.1") {
		t.Fatal("hashes under different salts should not match")
	}
}

func TestHashEmptyInput(t *testing.T) {
	a := mustAnonymizer(t)
	// We don't crash and we still produce a stable token.
	if got := a.Hash(""); got == "" {
		t.Fatal("empty input still must produce a non-empty hash")
	}
}

func mustAnonymizer(t *testing.T) *Anonymizer {
	t.Helper()
	a, err := New("static-test-salt")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func bytesOfLen(n int, fill byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = fill
	}
	return out
}
