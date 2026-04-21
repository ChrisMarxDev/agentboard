package agentboard

import "embed"

// Use `all:` to include files that start with `_` or `.` — Vite emits some chunks
// with underscore prefixes (e.g. _baseFor-*.js from lodash-es). Without `all:`,
// Go's embed.FS drops them, SPA fallback serves index.html in their place, and
// dynamic imports break with a MIME-type error.
//
//go:embed all:frontend/dist
var FrontendDist embed.FS
