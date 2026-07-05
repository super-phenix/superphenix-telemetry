// Package anonymizer hashes client identifiers (IP addresses, UIDs)
// using a static salt so that they can never be reversed to the original
// value and remain consistent across server restarts and instances.
package anonymizer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strings"
)

// Anonymizer turns an identifier into an opaque, non-reversible token.
//
// The salt is static and consistent across process restarts. This ensures
// that the same input always produces the same hash, allowing multiple
// server instances to interpret the same request with the same identifier.
type Anonymizer struct {
	salt []byte
}

// New returns an Anonymizer seeded with the provided salt.
func New(salt string) (*Anonymizer, error) {
	hash := sha256.Sum256([]byte(salt))
	return &Anonymizer{salt: hash[:]}, nil
}

// NewWithSalt is intended for tests that need deterministic output.
// Production code must use New.
func NewWithSalt(salt []byte) (*Anonymizer, error) {
	return &Anonymizer{salt: salt}, nil
}

// Hash returns the full hex-encoded HMAC-SHA256 of the normalized UID.
func (a *Anonymizer) Hash(identifier string) string {
	mac := hmac.New(sha256.New, a.salt)
	mac.Write([]byte(Normalize(identifier)))

	return hex.EncodeToString(mac.Sum(nil))
}

// ShortHash returns a short prefix of the full hash
func (a *Anonymizer) ShortHash(remoteAddr string) string {
	return a.Hash(remoteAddr)[:8]
}

// Normalize strips an optional port and normalizes the address so that
// "1.2.3.4:443" and "1.2.3.4" hash to the same value, and IPv6 addresses
// hash regardless of whether they are bracketed.
func Normalize(remoteAddr string) string {
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
