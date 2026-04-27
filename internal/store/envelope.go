// Package store implements the files-first data store described in
// spec-file-storage.md. Files on disk are the source of truth; in-memory
// indexes are derived state, rebuilt on startup.
package store

import (
	"encoding/json"
	"fmt"
)

// Shape names used in the _meta envelope and catalog index.
const (
	ShapeSingleton  = "singleton"
	ShapeCollection = "collection"
	ShapeStream     = "stream"
)

// Meta is the server-managed metadata block that wraps every value on
// disk. Agents only ever set Meta.Version (the CAS token, echoed from a
// prior read); every other field is server-owned and stripped on input.
type Meta struct {
	Version    string `json:"version"`               // server-monotonic timestamp
	CreatedAt  string `json:"created_at,omitempty"`  // first-write time, immutable
	ModifiedBy string `json:"modified_by,omitempty"` // actor name from auth token
	Shape      string `json:"shape,omitempty"`       // singleton | collection | stream
}

// Envelope is the on-disk JSON shape: every singleton and collection-item
// file contains exactly one envelope. The user's data lives under Value;
// server metadata under Meta. Uniform across primitives, objects, arrays.
type Envelope struct {
	Meta  Meta            `json:"_meta"`
	Value json.RawMessage `json:"value"`
}

// MarshalEnvelope produces the canonical on-disk bytes for an envelope.
// Pretty-printed (2-space indent) so files remain human-inspectable —
// the cost is negligible at our write volume and the win for debugging
// (and for any future "user opens the folder" affordance) is real.
func MarshalEnvelope(env *Envelope) ([]byte, error) {
	return json.MarshalIndent(env, "", "  ")
}

// UnmarshalEnvelope parses on-disk bytes. Tolerates missing _meta (treats
// as an empty Meta) so we can read files that pre-date a field addition,
// but a fully-populated envelope is always written back on the next
// write.
func UnmarshalEnvelope(b []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("store: parse envelope: %w", err)
	}
	if len(env.Value) == 0 {
		env.Value = json.RawMessage("null")
	}
	return &env, nil
}

// StripAgentMeta scrubs every Meta field except Version on incoming
// writes — agents may echo Version for CAS but must not forge timestamps,
// attribution, or shape. Returns the trusted Version (or "" if none was
// supplied).
func StripAgentMeta(env *Envelope) string {
	if env == nil {
		return ""
	}
	v := env.Meta.Version
	env.Meta = Meta{Version: v}
	return v
}
