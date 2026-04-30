// Package store implements the files-first content store described in
// spec-rework.md. Every doc on disk is a `.md` file with optional YAML
// frontmatter and an optional markdown body. The frontmatter holds
// structured fields; the body holds prose and JSX components. There
// is no separate "data" namespace — the project root is one tree.
//
// The legacy JSON envelope shape (`.json` with `_meta` + `value`) was
// retired in Cut 2 of the rewrite. The in-memory `Envelope` keeps the
// same name and shape; only the on-disk encoding changed.
package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Shape names recorded in _meta + the catalog index. After Cut 2 the
// shape is purely informational on docs (a folder of docs is a
// "collection," a single doc is a "singleton") — the same `.md`
// encoding handles both. Streams keep the distinct on-disk shape.
const (
	ShapeSingleton  = "singleton"
	ShapeCollection = "collection"
	ShapeStream     = "stream"
)

// Meta is the server-managed metadata block stamped into the
// `_meta` key of every doc's frontmatter. Agents echo Meta.Version
// for optimistic CAS; everything else is server-owned and stripped
// from agent input.
type Meta struct {
	Version    string `yaml:"version" json:"version"`
	CreatedAt  string `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	ModifiedBy string `yaml:"modified_by,omitempty" json:"modified_by,omitempty"`
	Shape      string `yaml:"shape,omitempty" json:"shape,omitempty"`
}

// Envelope is the in-memory representation of a doc. Body holds the
// markdown body verbatim (between the closing `---` and EOF). Value
// holds the user's structured fields as JSON — for object-shaped data
// the fields splat into top-level frontmatter alongside `_meta`; for
// primitives/arrays they live under a `value:` frontmatter key.
type Envelope struct {
	Meta  Meta            `json:"_meta"`
	Value json.RawMessage `json:"value"`
	Body  string          `json:"body,omitempty"`
}

// MarshalDoc produces the canonical on-disk bytes for an envelope:
//
//	---
//	<YAML frontmatter: _meta + user fields or value:>
//	---
//
//	<body>
//
// The trailing newline is always present so editors don't add one
// later (which would change the file hash and create spurious diffs).
func MarshalDoc(env *Envelope) ([]byte, error) {
	front := map[string]any{}

	// _meta first so it's at the top of the YAML block — readability.
	if env.Meta.Version != "" || env.Meta.CreatedAt != "" || env.Meta.ModifiedBy != "" || env.Meta.Shape != "" {
		front["_meta"] = env.Meta
	}

	// Splat the user's value into frontmatter. JSON objects normally
	// become top-level keys (so frontmatter reads naturally), but if
	// the object itself contains a `value` key the splat would
	// collide with the envelope's own `value:` slot on read — the
	// caller's `value` field would round-trip as the entire envelope
	// payload instead. Detect that case and nest the whole object
	// under `value:` so it round-trips intact. Primitives + arrays
	// + collisions all live under `value:`. (See ISSUES.md
	// `[needs-decision]` from Cut 7 — option (c).)
	if len(env.Value) > 0 && !bytes.Equal(env.Value, []byte("null")) {
		var asObject map[string]any
		canSplat := false
		if err := json.Unmarshal(env.Value, &asObject); err == nil && asObject != nil {
			canSplat = true
			if _, collides := asObject["value"]; collides {
				canSplat = false
			}
		}
		if canSplat {
			for k, v := range asObject {
				if k == "_meta" {
					// The user can't write _meta — server owns it.
					continue
				}
				front[k] = v
			}
		} else {
			var asAny any
			if err := json.Unmarshal(env.Value, &asAny); err != nil {
				return nil, fmt.Errorf("store: invalid value bytes: %w", err)
			}
			front["value"] = asAny
		}
	}

	frontBytes, err := yaml.Marshal(front)
	if err != nil {
		return nil, fmt.Errorf("store: marshal frontmatter: %w", err)
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(frontBytes)
	out.WriteString("---\n")
	if env.Body != "" {
		// Single blank line between frontmatter and body so the body
		// reads as its own block in editors.
		if !strings.HasPrefix(env.Body, "\n") {
			out.WriteString("\n")
		}
		out.WriteString(env.Body)
		if !strings.HasSuffix(env.Body, "\n") {
			out.WriteString("\n")
		}
	}
	return out.Bytes(), nil
}

// UnmarshalDoc parses on-disk bytes into an envelope. Tolerates:
//   - Missing frontmatter (returns env with empty Meta + null Value + body=full content)
//   - Body-only docs (entire file becomes Body)
//   - Frontmatter with neither `value:` nor object fields (Value = null)
//
// On any YAML parse error the function returns the error so the caller
// can fail the read; callers that want to be tolerant should fall back
// to "file is plain text" themselves.
func UnmarshalDoc(b []byte) (*Envelope, error) {
	env := &Envelope{Value: json.RawMessage("null")}

	if len(b) == 0 {
		return env, nil
	}

	// No leading `---` → entire file is body.
	if !bytes.HasPrefix(b, []byte("---\n")) && !bytes.HasPrefix(b, []byte("---\r\n")) {
		env.Body = string(b)
		return env, nil
	}

	// Strip opening marker.
	var rest []byte
	if bytes.HasPrefix(b, []byte("---\r\n")) {
		rest = b[5:]
	} else {
		rest = b[4:]
	}

	// Find closing `---` line.
	closeAt := closingMarker(rest)
	if closeAt < 0 {
		// Unterminated frontmatter → treat the whole file as body.
		env.Body = string(b)
		return env, nil
	}
	frontmatterRaw := rest[:closeAt]
	body := rest[closeAt:]
	// Skip the closing `---\n` (or `---\r\n`).
	if bytes.HasPrefix(body, []byte("---\r\n")) {
		body = body[5:]
	} else if bytes.HasPrefix(body, []byte("---\n")) {
		body = body[4:]
	} else if bytes.Equal(body, []byte("---")) {
		body = nil
	}
	// Trim a single leading blank line — MarshalDoc always inserts one.
	if bytes.HasPrefix(body, []byte("\n")) {
		body = body[1:]
	}
	env.Body = string(body)

	if len(bytes.TrimSpace(frontmatterRaw)) == 0 {
		return env, nil
	}

	parsed := map[string]any{}
	if err := yaml.Unmarshal(frontmatterRaw, &parsed); err != nil {
		return nil, fmt.Errorf("store: parse frontmatter: %w", err)
	}

	// Pull _meta out of the map and unmarshal it through JSON for the
	// strong-typed Meta struct. yaml.Unmarshal on map[string]any
	// produces map[string]any subtrees, so a JSON round-trip is the
	// cleanest way to coerce.
	if rawMeta, ok := parsed["_meta"]; ok {
		mb, _ := json.Marshal(rawMeta)
		_ = json.Unmarshal(mb, &env.Meta)
		delete(parsed, "_meta")
	}

	// Resolve the user's value:
	//   - If `value:` is present and it's the only key, that's the value.
	//   - Else the remaining keys become an object value.
	//   - If both `value:` and other keys are present, prefer the object form;
	//     callers shouldn't author both, but keep the keys as the canonical
	//     interpretation since they're what the dashboard reads.
	switch {
	case len(parsed) == 0:
		env.Value = json.RawMessage("null")
	case len(parsed) == 1:
		if v, has := parsed["value"]; has {
			vb, err := json.Marshal(v)
			if err != nil {
				return nil, fmt.Errorf("store: encode value: %w", err)
			}
			env.Value = vb
		} else {
			vb, err := json.Marshal(parsed)
			if err != nil {
				return nil, fmt.Errorf("store: encode frontmatter object: %w", err)
			}
			env.Value = vb
		}
	default:
		// Drop a stray `value:` if other keys are present — the object
		// shape wins. (See note above.)
		if _, hasValueKey := parsed["value"]; hasValueKey && len(parsed) > 1 {
			delete(parsed, "value")
		}
		vb, err := json.Marshal(parsed)
		if err != nil {
			return nil, fmt.Errorf("store: encode frontmatter object: %w", err)
		}
		env.Value = vb
	}

	return env, nil
}

// StripAgentMeta scrubs every Meta field except Version on incoming
// writes — agents may echo Version for CAS but must not forge
// timestamps, attribution, or shape. Returns the trusted Version (or
// "" if none was supplied).
func StripAgentMeta(env *Envelope) string {
	if env == nil {
		return ""
	}
	v := env.Meta.Version
	env.Meta = Meta{Version: v}
	return v
}

// closingMarker scans for a line containing exactly `---` and returns
// its byte offset (relative to the start of the slice). Returns -1 if
// no closing marker is found.
func closingMarker(body []byte) int {
	if bytes.HasPrefix(body, []byte("---\n")) || bytes.HasPrefix(body, []byte("---\r\n")) {
		return 0
	}
	for i := 0; i < len(body)-3; i++ {
		if body[i] != '\n' {
			continue
		}
		rest := body[i+1:]
		if bytes.HasPrefix(rest, []byte("---\n")) || bytes.HasPrefix(rest, []byte("---\r\n")) {
			return i + 1
		}
		if bytes.Equal(rest, []byte("---")) {
			return i + 1
		}
	}
	return -1
}

// MarshalEnvelope and UnmarshalEnvelope are kept as compat aliases so
// the rest of the package compiles without churn during Cut 2.
// Removed in Cut 3.
//
// Deprecated: use MarshalDoc / UnmarshalDoc.
func MarshalEnvelope(env *Envelope) ([]byte, error) { return MarshalDoc(env) }

// Deprecated: use MarshalDoc / UnmarshalDoc.
func UnmarshalEnvelope(b []byte) (*Envelope, error) { return UnmarshalDoc(b) }
