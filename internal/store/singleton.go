package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// SingletonOp encodes the caller's intent for handler-translation
// purposes. The store doesn't branch on it internally; it's recorded
// in events + the activity log for audit.
const (
	OpSet       = "SET"
	OpMerge     = "MERGE"
	OpDelete    = "DELETE"
	OpIncrement = "INCREMENT"
	OpCAS       = "CAS"
)

// ReadSingleton returns the on-disk envelope for a singleton key.
// Returns ErrNotFound if the key doesn't exist; WrongShapeError if a
// collection or stream lives at the same key.
func (s *Store) ReadSingleton(key string) (*Envelope, error) {
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
	case ShapeSingleton:
		// fall through
	default:
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeSingleton}
	}
	return readEnvelope(singletonPath(s.dataDir, key))
}

// Set replaces a singleton's value. Behavior depends on (existing-key,
// supplied-version):
//
//   - new key: writes (version is irrelevant, Meta.CreatedAt is set)
//   - existing key, no version supplied: ErrVersionRequired
//   - existing key, version "*": force-overwrite, no CAS check
//   - existing key, version matches current: writes
//   - existing key, version mismatches: ConflictError with current envelope
func (s *Store) Set(key string, value json.RawMessage, version, actor string) (*Envelope, error) {
	return s.singletonWrite(key, OpSet, actor, version, func(prev *Envelope) (json.RawMessage, error) {
		return value, nil
	})
}

// Merge applies an RFC 7396 JSON Merge Patch to a singleton's value.
// Server holds the path lock end-to-end so two concurrent merges
// compose deterministically; never returns ConflictError.
//
// Creates the key with the patch as the initial value if it doesn't
// exist (matches the SQLite store's existing behavior — agents rely on
// this for "set if missing, merge if present" patterns).
func (s *Store) Merge(key string, patch json.RawMessage, actor string) (*Envelope, error) {
	return s.singletonWrite(key, OpMerge, actor, "*", func(prev *Envelope) (json.RawMessage, error) {
		if prev == nil {
			return patch, nil
		}
		return JSONMergePatch(prev.Value, patch), nil
	})
}

// Increment adjusts a numeric singleton by the given delta. Creates the
// key at `by` if it doesn't exist (so `INCREMENT new_key by 1` yields 1).
// Returns ErrInvalidValue if the existing value isn't a JSON number.
func (s *Store) Increment(key string, by float64, actor string) (*Envelope, error) {
	return s.singletonWrite(key, OpIncrement, actor, "*", func(prev *Envelope) (json.RawMessage, error) {
		var current float64
		if prev != nil {
			if err := json.Unmarshal(prev.Value, &current); err != nil {
				return nil, fmt.Errorf("%w: existing value at %q is not a number", ErrInvalidValue, key)
			}
		}
		current += by
		return json.Marshal(current)
	})
}

// CAS performs an atomic compare-and-swap on a singleton's value.
// Returns CASError (current envelope embedded) when expected doesn't
// equal the on-disk value.
func (s *Store) CAS(key string, expected, next json.RawMessage, actor string) (*Envelope, error) {
	return s.singletonWrite(key, OpCAS, actor, "*", func(prev *Envelope) (json.RawMessage, error) {
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

// DeleteSingleton removes a singleton key. Idempotent — "already gone"
// returns nil. CAS via version: "" disables the check, "*" force-deletes.
func (s *Store) DeleteSingleton(key, version, actor string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return err
	}
	switch shape {
	case "":
		return nil // idempotent
	case ShapeSingleton:
		// fall through
	default:
		return &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeSingleton}
	}

	unlock := s.locks.Lock(key)
	defer unlock()

	current, readErr := readEnvelope(singletonPath(s.dataDir, key))
	if errors.Is(readErr, ErrNotFound) {
		return nil
	}
	if readErr != nil {
		return readErr
	}

	if err := checkVersion(current, version); err != nil {
		return err
	}

	if err := removePath(singletonPath(s.dataDir, key)); err != nil {
		return err
	}

	s.dropFromCatalog(key)
	s.recordHistory(key, "", OpDelete, actor, current, "")
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: OpDelete, Path: key, Shape: ShapeSingleton,
	})
	s.notify(Event{Key: key, Op: OpDelete, Shape: ShapeSingleton})
	return nil
}

// singletonWrite is the shared body of every singleton mutation: lock,
// read, transform, write, notify. The transform closure receives the
// current envelope (or nil if new) and returns the new value bytes.
//
// expectedVersion semantics:
//   - "" — write only if key doesn't exist; ErrVersionRequired otherwise
//   - "*" — force-write, skip CAS check (server-internal ops use this)
//   - any other string — strict CAS, ConflictError on mismatch
func (s *Store) singletonWrite(
	key, op, actor, expectedVersion string,
	transform func(prev *Envelope) (json.RawMessage, error),
) (*Envelope, error) {
	if err := ValidateKey(key); err != nil {
		return nil, err
	}
	shape, err := detectShape(s.dataDir, key)
	if err != nil {
		return nil, err
	}
	if shape != "" && shape != ShapeSingleton {
		return nil, &WrongShapeError{Key: key, Actual: shape, Attempt: ShapeSingleton}
	}

	unlock := s.locks.Lock(key)
	defer unlock()

	path := singletonPath(s.dataDir, key)
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

	createdAt := s.versions.Next() // fallback for never-existed keys
	if prev != nil && prev.Meta.CreatedAt != "" {
		createdAt = prev.Meta.CreatedAt
	}

	env := &Envelope{
		Meta: Meta{
			Version:    s.versions.Next(),
			CreatedAt:  createdAt,
			ModifiedBy: actor,
			Shape:      ShapeSingleton,
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

	s.touchCatalog(key, ShapeSingleton)

	// Audit + history are best-effort: failure here doesn't unwind the
	// primary write. The value on disk is the durable contract.
	s.recordHistory(key, "", op, actor, prev, env.Meta.Version)
	s.recordActivity(ActivityEntry{
		Actor: actor, Op: op, Path: key, Version: env.Meta.Version, Shape: ShapeSingleton,
	})

	s.notify(Event{Key: key, Op: op, Shape: ShapeSingleton, Version: env.Meta.Version})
	return env, nil
}

// readEnvelope opens a JSON envelope file, parsing in one shot.
// Returns ErrNotFound (translated from os.IsNotExist) for missing files
// so callers don't need to import os.
func readEnvelope(path string) (*Envelope, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return UnmarshalEnvelope(b)
}

// checkVersion implements the CAS precondition rules.
func checkVersion(prev *Envelope, expected string) error {
	if expected == "*" {
		return nil
	}
	if prev == nil {
		// Key doesn't exist; only "" (no precondition) and "*" allow
		// writing. Distinguish: empty means "no version asserted",
		// allowed for fresh keys.
		if expected == "" {
			return nil
		}
		// Caller asserted a version on a missing key — ConflictError
		// with Current=nil so handlers can render "this key was deleted
		// or never existed".
		return &ConflictError{Current: nil, YourVersion: expected}
	}
	if expected == "" {
		return ErrVersionRequired
	}
	if prev.Meta.Version != expected {
		return &ConflictError{Current: prev, YourVersion: expected}
	}
	return nil
}

// jsonDeepEqual returns true iff two JSON byte slices represent the
// same logical value (whitespace, key order, and trailing-newline
// independent). Used by CAS — the spec says "same value", not "same
// bytes".
func jsonDeepEqual(a, b json.RawMessage) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	ab, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(ab) == string(bb)
}
