package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. These are the "interactive login" profile — good
// enough for admin login on commodity VPS hardware, tuned to take ~50–100ms
// on a 2020-era CPU so brute-force costs the attacker real time per try.
//
// If you change these, bump the encoded prefix version ($argon2id$v=19$)
// so old hashes are still verifiable. Current encoded form stays stable
// across parameter tweaks because HashPassword always reads params from
// the stored hash on verification.
const (
	argonMemory      = 64 * 1024 // 64 MiB
	argonIterations  = 3
	argonParallelism = 2
	argonKeyLen      = 32
	argonSaltLen     = 16
)

// ErrInvalidPassword is returned when VerifyPassword fails on hash mismatch.
// Callers should NOT distinguish between "wrong password" and "no such user"
// in HTTP responses — return a generic 401 in both cases to avoid user
// enumeration.
var ErrInvalidPassword = errors.New("invalid password")

// ErrMalformedHash is returned when a stored hash can't be parsed. Only
// happens on corrupted DB rows.
var ErrMalformedHash = errors.New("malformed password hash")

// HashPassword returns an encoded argon2id hash in the standard $argon2id$
// format. Safe to store in a text column.
func HashPassword(plaintext string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plaintext), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)

	// Format: $argon2id$v=19$m=<mem>,t=<iter>,p=<par>$<salt>$<key>
	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	keyB64 := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		saltB64, keyB64), nil
}

// VerifyPassword checks a plaintext password against a stored hash in
// constant time. Returns ErrInvalidPassword on mismatch; other errors
// indicate a malformed hash.
func VerifyPassword(plaintext, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return ErrMalformedHash
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrMalformedHash
	}
	expectedKey, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return ErrMalformedHash
	}

	actualKey := argon2.IDKey([]byte(plaintext), salt, iterations, memory, parallelism, uint32(len(expectedKey)))
	if subtle.ConstantTimeCompare(expectedKey, actualKey) != 1 {
		return ErrInvalidPassword
	}
	return nil
}
