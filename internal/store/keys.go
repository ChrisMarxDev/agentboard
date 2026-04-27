package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// keyPattern is intentionally permissive: agents pick keys; we only
// reject the things that would break the filesystem mapping or trick
// path resolution. Length cap is generous — keys this long are a smell
// but not a safety issue.
const maxKeyLen = 255

// ValidateKey enforces the reserved-character rules from the spec.
// Returns a structured error so handlers can render a poka-yoke 400
// (CORE_GUIDELINES §12).
func ValidateKey(k string) error {
	if k == "" {
		return fmt.Errorf("key: empty")
	}
	if len(k) > maxKeyLen {
		return fmt.Errorf("key: too long (%d > %d)", len(k), maxKeyLen)
	}
	if strings.ContainsRune(k, 0) {
		return fmt.Errorf("key: contains null byte")
	}
	if strings.ContainsAny(k, `/\`) {
		return fmt.Errorf("key: contains path separator (/ or \\)")
	}
	if strings.HasPrefix(k, ".") {
		return fmt.Errorf("key: leading dot reserved")
	}
	if k == "." || k == ".." || strings.Contains(k, "..") {
		return fmt.Errorf("key: contains traversal sequence")
	}
	return nil
}

// ValidateID enforces the same rules as keys, applied to collection-item
// IDs. The on-disk filename is `<dataDir>/<key>/<id>.json` so IDs
// inherit every constraint that protects the path.
func ValidateID(id string) error {
	if err := ValidateKey(id); err != nil {
		return fmt.Errorf("id: %s", strings.TrimPrefix(err.Error(), "key: "))
	}
	return nil
}

// singletonPath returns the absolute file path for a singleton key.
// Layout is flat: dotted keys are filenames literally, dots are NOT
// directory separators. This avoids the "is dev.metrics a file or
// directory?" ambiguity that hierarchical layouts force on us.
func singletonPath(dataDir, key string) string {
	return filepath.Join(dataDir, key+".json")
}

// collectionDir returns the absolute directory for a collection key.
// Each item lives at <collectionDir>/<id>.json.
func collectionDir(dataDir, key string) string {
	return filepath.Join(dataDir, key)
}

// collectionItemPath returns the absolute path for one item in a
// collection.
func collectionItemPath(dataDir, key, id string) string {
	return filepath.Join(dataDir, key, id+".json")
}

// streamPath returns the absolute file path for a stream's active
// segment. Rotated segments live alongside as <key>.1.ndjson .. .5.ndjson.
func streamPath(dataDir, key string) string {
	return filepath.Join(dataDir, key+".ndjson")
}

// streamRotatedPath returns the path for a rotated stream segment. n
// must be ≥ 1; n == 0 is the active path (use streamPath for that).
func streamRotatedPath(dataDir, key string, n int) string {
	return filepath.Join(dataDir, fmt.Sprintf("%s.%d.ndjson", key, n))
}

// detectShape inspects the filesystem to determine which shape (if any)
// already exists for a key. A key has at most one shape; first-write
// pins it. Returns "" if no file exists yet for this key.
//
// Order matters: we check the collection directory before the singleton
// file because, in the rare case where someone created a path conflict
// out-of-band, the directory is the more committed structure.
func detectShape(dataDir, key string) (string, error) {
	if _, err := os.Stat(collectionDir(dataDir, key)); err == nil {
		return ShapeCollection, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if _, err := os.Stat(singletonPath(dataDir, key)); err == nil {
		return ShapeSingleton, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if _, err := os.Stat(streamPath(dataDir, key)); err == nil {
		return ShapeStream, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return "", nil
}
