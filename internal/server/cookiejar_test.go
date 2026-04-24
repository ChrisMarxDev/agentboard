package server

import "net/http/cookiejar"

// newCookieJar is a tiny helper used by handlers_view_test.go.
func newCookieJar() (*cookiejar.Jar, error) { return cookiejar.New(nil) }
