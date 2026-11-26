package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// DefaultOpaqueTokenBytes is 32 bytes (256 bits) of CSPRNG entropy per security baseline.
const DefaultOpaqueTokenBytes = 32

// GenerateOpaqueToken generates a URL-safe base64 token with numBytes of random entropy.
func GenerateOpaqueToken(numBytes int) (string, error) {
	if numBytes <= 0 {
		numBytes = DefaultOpaqueTokenBytes
	}
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashTokenSHA256 computes a lowercase hex-encoded SHA-256 digest of a raw opaque token.
func HashTokenSHA256(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ConstantTimeEqual compares two strings without leaking timing information.
func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
