// Package anonymizer hashes client identifiers (IP addresses) using a
// static salt so that they can never be reversed to the original
// value and remain consistent across server restarts and instances.
package anonymizer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
)

const saltBytes = 32

// Anonymizer turns a remote address into an opaque, non-reversible token.
//
// The salt is static and consistent across process restarts. This ensures
// that the same input always produces the same hash, allowing multiple
// server instances to interpret the same request with the same identifier.
type Anonymizer struct {
	salt []byte
}

// New returns an Anonymizer seeded with a static salt.
func New() (*Anonymizer, error) {
	// Use a fixed static salt to ensure consistent hashing across instances.
	// This replaces the previous random per-process salt.
	staticSalt := []byte("superphenix-telemetry-salt-v1")
	h := sha256.Sum256(staticSalt)
	return &Anonymizer{salt: h[:]}, nil
}

// NewWithSalt is intended for tests that need deterministic output.
// Production code must use New.
func NewWithSalt(salt []byte) (*Anonymizer, error) {
	return &Anonymizer{salt: salt}, nil
}

// Hash returns the full hex-encoded HMAC-SHA256 of the normalized address.
// Suitable as a stable key for rate limiting within the lifetime of the process.
func (a *Anonymizer) Hash(remoteAddr string) string {
	mac := hmac.New(sha256.New, a.salt)
	mac.Write([]byte(normalize(remoteAddr)))

	return hex.EncodeToString(mac.Sum(nil))
}

// ShortHash returns a short prefix of the full hash, intended for log lines
// where we want a stable but low-resolution identifier. 8 hex chars (32 bits)
// is enough to spot repeat offenders in logs while not being unique enough
// to single anybody out across the IP space.
func (a *Anonymizer) ShortHash(remoteAddr string) string {
	return a.Hash(remoteAddr)[:8]
}

// normalize strips an optional port and normalizes the address so that
// "1.2.3.4:443" and "1.2.3.4" hash to the same value, and IPv6 addresses
// hash regardless of whether they are bracketed.
func normalize(remoteAddr string) string {
	// Remove blank spaces
	addr := strings.TrimSpace(remoteAddr)
	if addr == "" {
		return ""
	}

	// Remove the source port
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}

	// Remove brackets from IPv6 addresses
	addr = strings.Trim(addr, "[]")
	if ip := net.ParseIP(addr); ip != nil {
		return ip.String()
	}

	return addr
}
