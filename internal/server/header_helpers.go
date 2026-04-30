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

// requestIsSecure reports whether the inbound request is over HTTPS,
// taking proxy hops into account. Cloudflare Tunnel (and most reverse
// proxies) terminate TLS at the edge and forward plain HTTP to the
// backend — `r.TLS` is nil even though the browser ↔ proxy leg is
// HTTPS. The X-Forwarded-Proto header is the proxy's signal to the
// origin that the original scheme was HTTPS.
//
// Used by the cookie-set helpers so the `Secure` flag matches the
// browser-facing scheme. Without it, browsers in strict-cookies
// modes drop the Set-Cookie header silently and every subsequent
// request lands without auth — the "session expired" redirect loop
// the dogfood hit through the Cloudflare Tunnel.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		// X-Forwarded-Proto can be a comma-separated list when there
		// are multiple proxies; the leftmost is the client's hop.
		first := strings.TrimSpace(strings.Split(proto, ",")[0])
		if strings.EqualFold(first, "https") {
			return true
		}
	}
	if r.Header.Get("CF-Visitor") != "" {
		// Cloudflare's CF-Visitor header has the form `{"scheme":"https"}`;
		// presence alone is a strong signal we're behind CF and the
		// edge handled TLS. The header is documented at
		// https://developers.cloudflare.com/fundamentals/reference/http-request-headers/#cf-visitor
		// We don't bother JSON-parsing — if X-Forwarded-Proto wasn't
		// set, default to true since CF tunnels always front HTTPS.
		return true
	}
	return false
}
