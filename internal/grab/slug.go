package grab

import (
	"strings"
	"unicode"
)

// Slug converts a Card title into a stable kebab-case ID. Mirrors the
// frontend's slug() in lib/grab.ts — both must agree or picks won't resolve.
//
// Rules:
//   - lowercase
//   - runs of non-alphanumerics → single "-"
//   - leading/trailing "-" trimmed
//   - empty input → empty output (caller decides how to handle)
func Slug(title string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
