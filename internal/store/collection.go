package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Collection op names — recorded on Events and in the activity log.
const (
	OpUpsertByID = "UPSERT_BY_ID"
	OpMergeByID  = "MERGE_BY_ID"
	OpDeleteByID = "DELETE_BY_ID"
)

// CollectionItem pairs an envelope with its ID for list responses.
type CollectionItem struct {
	ID       string    `json:"id"`
	Envelope *Envelope `json:"envelope"`
}

// ListCollection returns every item in a collection, sorted by ID for
// deterministic output. Pagination (after/limit) is the caller's job;
// at the storage layer we just return everything.
func (s *Store) ListCollection(key string) ([]CollectionItem, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	switch shape {
	case "":
		return nil, ErrNotFound
	case ShapeCollection:
		// fall through
	default:
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeCollection}
	}

	dir := collectionDir(s.dataDir, key)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	items := make([]CollectionItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		env, err := readEnvelope(collectionItemPath(s.dataDir, key, id))
		if err != nil {
			// Skip unreadable items rather than failing the whole list —
			// a partially-written file (very brief window during a
			// crash) shouldn't black-hole every other agent's read.
			continue
		}
		items = append(items, CollectionItem{ID: id, Envelope: env})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// ReadItem returns one item from a collection. ErrNotFound covers both
// "no such collection" and "no such ID in this collection" — handlers
// translate the same 404 in either case.
func (s *Store) ReadItem(key, id string) (*Envelope, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	switch shape {
	case "", ShapeCollection:
		// fall through
	default:
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeCollection}
	}
	return readEnvelope(collectionItemPath(s.dataDir, key, id))
}

// UpsertItem creates or replaces a single item by ID. Per-(key,id) lock
// scope means two writes to different IDs in the same collection never
// block each other — this is the core property that makes collections
// conflict-free for distinct IDs.
func (s *Store) UpsertItem(key, id string, value json.RawMessage, version, actor string) (*Envelope, error) {
	return s.itemWrite(key, id, OpUpsertByID, actor, version, func(prev *Envelope) (json.RawMessage, error) {
		return value, nil
	})
}

// MergeItem applies an RFC 7396 patch to a single collection item.
// Server retries under lock; never returns ConflictError. If the item
// doesn't exist, the patch becomes the initial value (matches the
// Merge semantic at the singleton layer for symmetry).
func (s *Store) MergeItem(key, id string, patch json.RawMessage, actor string) (*Envelope, error) {
	return s.itemWrite(key, id, OpMergeByID, actor, "*", func(prev *Envelope) (json.RawMessage, error) {
		if prev == nil {
			return patch, nil
		}
		return JSONMergePatch(prev.Value, patch), nil
	})
}

// CASItem performs CAS on one collection item. Identical semantics to
// the singleton CAS: returns CASError with current envelope on
// expected-value mismatch.
func (s *Store) CASItem(key, id string, expected, next json.RawMessage, actor string) (*Envelope, error) {
	return s.itemWrite(key, id, OpCAS, actor, "*", func(prev *Envelope) (json.RawMessage, error) {
		var prevVal json.RawMessage
		if prev != nil {
			prevVal = prev.Value
		}
		if !jsonDeepEqual(prevVal, expected) {
			return nil, &CASError{Current: prev}
		}
		return next, nil
	})
}

// DeleteItem removes one collection item. Idempotent.
func (s *Store) DeleteItem(key, id, version, actor string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := ValidateID(id); err != nil {
		return err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return err
	}
	if shape != "" && shape != ShapeCollection {
		return &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeCollection}
	}

	lockKey := key + "/" + id
	unlock := s.locks.Lock(lockKey)
	defer unlock()

	path := collectionItemPath(s.dataDir, key, id)
	current, readErr := readEnvelope(path)
	if errors.Is(readErr, ErrNotFound) {
		return nil
	}
	if readErr != nil {
		return readErr
	}
	if err := checkVersion(current, version); err != nil {
		return err
	}

	if err := removePath(path); err != nil {
		return err
	}

	s.touchCatalog(key, ShapeCollection)
	s.recordHistory(key, id, OpDeleteByID, actor, current, "")
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: OpDeleteByID, Path: key + "/" + id, Shape: ShapeCollection,
	})
	s.notify(Event{Key: key, ID: id, Op: OpDeleteByID, Shape: ShapeCollection})

	// Best-effort cleanup: if this was the last item, remove the empty
	// directory so detectShape() correctly reports "" (no shape) on a
	// fresh write. Ignore errors — leaving an empty dir is safe.
	if entries, err := os.ReadDir(collectionDir(s.dataDir, key)); err == nil && len(entries) == 0 {
		_ = os.Remove(collectionDir(s.dataDir, key))
	}
	return nil
}

// DeleteCollection removes the whole collection. Caller-confirmed at
// the handler layer (we don't know "intent" here). Idempotent.
func (s *Store) DeleteCollection(key, actor string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return err
	}
	switch shape {
	case "":
		return nil
	case ShapeCollection:
		// fall through
	default:
		return &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeCollection}
	}

	// Lock the collection-level key (no slash) so concurrent per-item
	// writes block during a wholesale delete.
	unlock := s.locks.Lock(key)
	defer unlock()

	if err := removeDirRecursive(collectionDir(s.dataDir, key)); err != nil {
		return err
	}
	s.dropFromCatalog(key)
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: OpDelete, Path: key, Shape: ShapeCollection,
	})
	s.notify(Event{Key: key, Op: OpDelete, Shape: ShapeCollection})
	return nil
}

// itemWrite is the per-item analog of singletonWrite. Same lock-read-
// transform-write-notify shape; lock scope is `key/id` so siblings
// don't contend.
func (s *Store) itemWrite(
	key, id, op, actor, expectedVersion string,
	transform func(prev *Envelope) (json.RawMessage, error),
) (*Envelope, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	if shape != "" && shape != ShapeCollection {
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeCollection}
	}

	lockKey := key + "/" + id
	unlock := s.locks.Lock(lockKey)
	defer unlock()

	path := collectionItemPath(s.dataDir, key, id)
	prev, readErr := readEnvelope(path)
	if readErr != nil && !errors.Is(readErr, ErrNotFound) {
		return nil, readErr
	}

	if err := checkVersion(prev, expectedVersion); err != nil {
		return nil, err
	}

	newValue, err := transform(prev)
	if err != nil {
		return nil, err
	}
	if !json.Valid(newValue) {
		return nil, fmt.Errorf("%w: result is not valid JSON", ErrInvalidValue)
	}

	createdAt := s.versions.Next()
	if prev != nil && prev.Meta.CreatedAt != "" {
		createdAt = prev.Meta.CreatedAt
	}

	env := &Envelope{
		Meta: Meta{
			Version:    s.versions.Next(),
			CreatedAt:  createdAt,
			ModifiedBy: actor,
			Shape:      ShapeCollection,
		},
		Value: newValue,
	}

	bytes, err := MarshalEnvelope(env)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, bytes); err != nil {
		return nil, err
	}

	s.touchCatalog(key, ShapeCollection)
	s.recordHistory(key, id, op, actor, prev, env.Meta.Version)
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: op, Path: key + "/" + id, Version: env.Meta.Version, Shape: ShapeCollection,
	})

	s.notify(Event{Key: key, ID: id, Op: op, Shape: ShapeCollection, Version: env.Meta.Version})
	return env, nil
}
