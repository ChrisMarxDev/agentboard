package auth

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
)

// usernameRE matches a valid username. The rules mirror @mentions in
// popular chat tools: lowercase letters, digits, underscores, and hyphens;
// must start with a letter; 1-32 characters.
//
// The same regex is used client-side for autocomplete and validation — keep
// these in lockstep with any frontend equivalent.
var usernameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// ErrInvalidUsername is returned when a username doesn't match usernameRE.
var ErrInvalidUsername = errors.New("invalid username: must start with a lowercase letter and contain only a-z, 0-9, _ or -; max 32 chars")

// ValidateUsername reports whether s is a valid username.
func ValidateUsername(s string) error {
	if !usernameRE.MatchString(s) {
		return ErrInvalidUsername
	}
	return nil
}

// avatarColorForUsername returns a deterministic HSL color string for a
// username. Stored on the user row so UI logic changes don't shift colors
// for existing users, and renames keep the historical color if the admin
// copies it across.
func avatarColorForUsername(username string) string {
	h := sha256.Sum256([]byte(username))
	// Hue 0-359, saturation 60-80%, lightness 50-60% — keeps colors readable
	// against both light and dark backgrounds.
	hue := int(h[0])<<8 | int(h[1])
	sat := 60 + int(h[2])%20
	lit := 50 + int(h[3])%10
	return fmt.Sprintf("hsl(%d, %d%%, %d%%)", hue%360, sat, lit)
}
