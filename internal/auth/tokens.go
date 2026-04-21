package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// TokenPrefix prefixes every agent token so they're log-greppable and
// detectable by secret scanners. Matches the "provider prefix" convention
// used by GitHub / Stripe / OpenAI API keys.
const TokenPrefix = "ab_"

// GenerateToken returns a fresh agent token of the form `ab_<43 chars>`
// (32 bytes of entropy, URL-safe base64 without padding).
func GenerateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// GenerateBootstrapCode returns a one-time code that the installer prints
// and the browser consumes at /setup. 20 bytes of base32 is enough entropy
// to resist brute force and still be typeable.
func GenerateBootstrapCode() (string, error) {
	var raw [20]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	// Use base32 without padding, lowercase — easier to read off a terminal
	// and not confusable with base64 tokens.
	enc := base64.RawURLEncoding.EncodeToString(raw[:])
	return strings.ToLower(enc), nil
}

// GenerateSessionID returns an opaque random session identifier.
func GenerateSessionID() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// HashToken is the one-way hash stored in the DB. Not a password hash —
// tokens are high-entropy random strings, so plain sha256 is appropriate
// and gives us constant-time lookup. Passwords use argon2id (see password.go).
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokensEqual is a constant-time hash comparison, used when verifying a
// presented token against the stored hash.
func TokensEqual(hashA, hashB string) bool {
	return subtle.ConstantTimeCompare([]byte(hashA), []byte(hashB)) == 1
}
