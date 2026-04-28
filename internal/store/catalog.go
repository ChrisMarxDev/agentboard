package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CatalogEntry describes one key in the in-memory catalog index. The
// rendering of /api/index pulls these directly. Values are NOT cached
// here — agents fetch the value separately. This keeps the catalog
// payload bounded and lets us answer "what exists?" in one cheap call.
type CatalogEntry struct {
	Key      string `json:"key"`
	Shape    string `json:"shape"`
	Version  string `json:"version,omitempty"`
	Size     int64  `json:"size,omitempty"`      // bytes on disk
	Type     string `json:"type,omitempty"`      // inferred JSON type (singleton only)
	Count    int    `json:"count,omitempty"`     // collection only: item count
	LineCount int  `json:"line_count,omitempty"` // stream only: lines in active segment
	ModifiedAt string `json:"modified_at,omitempty"` // RFC3339Nano (== version for typed shapes)
}

// catalog is the in-memory index. Built on startup by walkDataDir;
// kept in sync via touchCatalog from every write path.
type catalog struct {
	mu sync.RWMutex
	m  map[string]CatalogEntry
}

// LoadCatalog walks dataDir once, parses every envelope or stream
// segment, and returns a populated catalog. Errors on individual files
// are tolerated: a malformed file becomes invisible to /api/index but
// doesn't fail the whole startup. Logs are the operator's job (the
// caller can wrap if loud failure is preferred).
func LoadCatalog(dataDir string) (*catalog, error) {
	c := &catalog{m: map[string]CatalogEntry{}}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dataDir, name)
		if e.IsDir() {
			// collection
			items, _ := os.ReadDir(full)
			count := 0
			latest := ""
			for _, it := range items {
				if it.IsDir() || !strings.HasSuffix(it.Name(), ".md") {
					continue
				}
				count++
				if env, err := readEnvelope(filepath.Join(full, it.Name())); err == nil {
					if env.Meta.Version > latest {
						latest = env.Meta.Version
					}
				}
			}
			c.m[name] = CatalogEntry{
				Key: name, Shape: ShapeCollection, Count: count,
				Version: latest, ModifiedAt: latest,
			}
			continue
		}

		switch {
		case strings.HasSuffix(name, ".md"):
			key := strings.TrimSuffix(name, ".md")
			fi, _ := e.Info()
			env, err := readEnvelope(full)
			if err != nil {
				continue
			}
			c.m[key] = CatalogEntry{
				Key: key, Shape: ShapeSingleton,
				Version: env.Meta.Version, ModifiedAt: env.Meta.Version,
				Size: fi.Size(), Type: inferJSONType(env.Value),
			}
		case strings.HasSuffix(name, ".ndjson") && !isRotatedSegment(name):
			key := strings.TrimSuffix(name, ".ndjson")
			fi, _ := e.Info()
			c.m[key] = CatalogEntry{
				Key: key, Shape: ShapeStream,
				Size: fi.Size(),
				ModifiedAt: fi.ModTime().UTC().Format(time.RFC3339Nano),
			}
		}
	}
	return c, nil
}

// isRotatedSegment returns true for "<key>.N.ndjson" where N is a
// positive integer. We strip these from the catalog (they're shadows
// of an active stream).
func isRotatedSegment(name string) bool {
	if !strings.HasSuffix(name, ".ndjson") {
		return false
	}
	stripped := strings.TrimSuffix(name, ".ndjson")
	dot := strings.LastIndex(stripped, ".")
	if dot < 0 {
		return false
	}
	suffix := stripped[dot+1:]
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// inferJSONType returns one of object | array | string | number |
// boolean | null | unknown for a JSON byte slice. Mirrors what the
// SQLite store's InferSchema returned for the type field; agents and
// the dashboard rely on this for "what kind of value is this?" hints.
func inferJSONType(b json.RawMessage) string {
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "unknown"
	}
	switch t[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// Catalog returns a snapshot of the in-memory catalog.
func (s *Store) Catalog() []CatalogEntry {
	s.cat.mu.RLock()
	defer s.cat.mu.RUnlock()
	out := make([]CatalogEntry, 0, len(s.cat.m))
	for _, e := range s.cat.m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// CatalogGet returns the entry for one key, ok=false if missing.
func (s *Store) CatalogGet(key string) (CatalogEntry, bool) {
	s.cat.mu.RLock()
	defer s.cat.mu.RUnlock()
	e, ok := s.cat.m[key]
	return e, ok
}

// touchCatalog updates the in-memory entry for a key after a write.
// Called from every successful write path (singleton, collection,
// stream). Cheap (one map insert under a write lock); the catalog is
// not authoritative — if this fails or is skipped, /api/index drifts
// but no on-disk state is wrong.
func (s *Store) touchCatalog(key string, shape string) {
	s.cat.mu.Lock()
	defer s.cat.mu.Unlock()
	switch shape {
	case ShapeSingleton:
		path := singletonPath(s.dataDir, key)
		fi, err := os.Stat(path)
		if err != nil {
			delete(s.cat.m, key)
			return
		}
		env, err := readEnvelope(path)
		if err != nil {
			return
		}
		s.cat.m[key] = CatalogEntry{
			Key: key, Shape: ShapeSingleton,
			Version: env.Meta.Version, ModifiedAt: env.Meta.Version,
			Size: fi.Size(), Type: inferJSONType(env.Value),
		}
	case ShapeCollection:
		dir := collectionDir(s.dataDir, key)
		items, err := os.ReadDir(dir)
		if err != nil {
			delete(s.cat.m, key)
			return
		}
		count := 0
		latest := ""
		for _, it := range items {
			if it.IsDir() || !strings.HasSuffix(it.Name(), ".md") {
				continue
			}
			count++
			if env, err := readEnvelope(filepath.Join(dir, it.Name())); err == nil {
				if env.Meta.Version > latest {
					latest = env.Meta.Version
				}
			}
		}
		if count == 0 {
			delete(s.cat.m, key)
			return
		}
		s.cat.m[key] = CatalogEntry{
			Key: key, Shape: ShapeCollection, Count: count,
			Version: latest, ModifiedAt: latest,
		}
	case ShapeStream:
		path := streamPath(s.dataDir, key)
		fi, err := os.Stat(path)
		if err != nil {
			delete(s.cat.m, key)
			return
		}
		s.cat.m[key] = CatalogEntry{
			Key: key, Shape: ShapeStream,
			Size: fi.Size(),
			ModifiedAt: fi.ModTime().UTC().Format(time.RFC3339Nano),
		}
	}
}

// dropFromCatalog removes a key after a wholesale delete.
func (s *Store) dropFromCatalog(key string) {
	s.cat.mu.Lock()
	defer s.cat.mu.Unlock()
	delete(s.cat.m, key)
}
