package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. Time=1 / memory=64MiB / threads=4 / 32-byte key
// is the modern OWASP-aligned default. Encoded into the stored hash so
// future tuning won't strand existing passwords; VerifyPassword reads
// the parameters from the stored value and uses those.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // 64 MiB
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16

	// MinPasswordLen is enforced at the SetPassword call site. 10 keeps
	// the bar above "trivial typed twice" without being so strict that
	// the operator types it on a phone in a panic and bounces.
	MinPasswordLen = 10
)

// ErrWeakPassword is returned by SetPassword when the candidate fails
// the trivial length check. It's not the only thing the UI should
// guard against — it's the floor.
var ErrWeakPassword = fmt.Errorf("password must be at least %d characters", MinPasswordLen)

// HashPassword runs Argon2id over the plaintext with a fresh salt.
// Returns a self-describing string of the form
//
//	$argon2id$v=19$m=65536,t=1,p=4$<salt-b64>$<key-b64>
//
// matching the canonical Argon2 reference encoding so any standard
// verifier can read it. We only ever compare via VerifyPassword, but
// using the standard encoding means we never have to migrate on a
// parameter tuning.
func HashPassword(plain string) (string, error) {
	if len(plain) < MinPasswordLen {
		return "", ErrWeakPassword
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares a candidate plaintext against the stored
// hash in constant time. Returns false (never an error) for any
// malformed or unverifiable hash so callers don't accidentally branch
// "wrong password" vs "stored hash is corrupt" — both should look
// identical to an attacker.
//
// Constant-time guarantee depends on argon2.IDKey being constant-time
// for a given parameter set + subtle.ConstantTimeCompare on the final
// keys. Different parameter sets do leak (different KDF runtimes), but
// we only ever store one set, so in practice every verify takes the
// same time.
func VerifyPassword(plain, stored string) bool {
	parts := strings.Split(stored, "$")
	// "" "argon2id" "v=19" "m=...,t=...,p=..." "salt" "key" => 6 parts
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var m, t uint32
	var p uint8
	if err := parseArgonParams(parts[3], &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parseArgonParams pulls m/t/p out of the "m=...,t=...,p=..." segment
// of a stored hash. Returns an error on anything malformed.
func parseArgonParams(seg string, m, t *uint32, p *uint8) error {
	for _, kv := range strings.Split(seg, ",") {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			return errors.New("bad param segment")
		}
		key, val := kv[:eq], kv[eq+1:]
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return fmt.Errorf("bad %s: %w", key, err)
		}
		switch key {
		case "m":
			*m = uint32(n)
		case "t":
			*t = uint32(n)
		case "p":
			if n > 255 {
				return errors.New("p out of range")
			}
			*p = uint8(n)
		}
	}
	if *m == 0 || *t == 0 || *p == 0 {
		return errors.New("missing argon param")
	}
	return nil
}
