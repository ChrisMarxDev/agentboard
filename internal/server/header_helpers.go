package server

// Small HTTP-header helpers that several handlers share. Used to live
// in handlers_data.go; pulled out into their own file when the legacy
// data layer was deleted in Cut 1.

import (
	"net/http"
	"strings"
)

// ifMatch normalises the If-Match request header for optimistic-CAS
// writes — strips weak-validator prefix and surrounding quotes so the
// store can compare it byte-for-byte against a stored version.
func ifMatch(r *http.Request) string {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	v = strings.TrimPrefix(v, "W/")
	v = strings.Trim(v, `"`)
	return v
}
