package data

import (
	"database/sql"
	"encoding/json"
)

// checkIfMatch validates the caller's expected updated_at against the current
// row inside an open transaction. Returns nil when the check passes (or when
// expected is empty — no check requested). Returns ErrStale on mismatch and
// ErrNotFoundForMatch when the row doesn't exist but a check was requested.
//
// Caller holds s.mu and the tx is open.
func (s *SQLiteStore) checkIfMatch(tx *sql.Tx, key, expected string) error {
	if expected == "" {
		return nil
	}
	var current string
	err := tx.QueryRow(`SELECT updated_at FROM data WHERE key = ?`, key).Scan(&current)
	if err == sql.ErrNoRows {
		return ErrNotFoundForMatch
	}
	if err != nil {
		return err
	}
	if current != expected {
		return ErrStale
	}
	return nil
}

// SetIfMatch is Set with an optimistic-concurrency precondition. See
// DataStore docs for the contract.
func (s *SQLiteStore) SetIfMatch(key string, value json.RawMessage, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	now := s.now()
	_, err = tx.Exec(
		`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		key, string(value), now, source,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, value)
	return nil
}

// MergeIfMatch is Merge with an optimistic-concurrency precondition.
func (s *SQLiteStore) MergeIfMatch(key string, patch json.RawMessage, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	now := s.now()
	var existing string
	err = tx.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&existing)
	if err == sql.ErrNoRows {
		_, err = tx.Exec(
			`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)`,
			key, string(patch), now, source,
		)
	} else if err != nil {
		return err
	} else {
		merged := jsonMergePatch([]byte(existing), patch)
		_, err = tx.Exec(
			`UPDATE data SET value = ?, updated_at = ?, updated_by = ? WHERE key = ?`,
			string(merged), now, source, key,
		)
	}
	if err != nil {
		return err
	}

	var finalValue string
	_ = tx.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&finalValue)

	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, json.RawMessage(finalValue))
	return nil
}

// AppendIfMatch is Append with an optimistic-concurrency precondition.
func (s *SQLiteStore) AppendIfMatch(key string, item json.RawMessage, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}
	arr = append(arr, item)

	final, err := json.Marshal(arr)
	if err != nil {
		return err
	}

	now := s.now()
	_, err = tx.Exec(
		`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		key, string(final), now, source,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, final)
	return nil
}

// DeleteIfMatch is Delete with an optimistic-concurrency precondition.
func (s *SQLiteStore) DeleteIfMatch(key, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM data WHERE key = ?`, key)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, nil)
	return nil
}

// UpsertByIdIfMatch is UpsertById with an optimistic-concurrency precondition.
func (s *SQLiteStore) UpsertByIdIfMatch(key, id string, item json.RawMessage, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	found := false
	for i, existing := range arr {
		var obj map[string]any
		if err := json.Unmarshal(existing, &obj); err != nil {
			continue
		}
		if objID, ok := obj["id"].(string); ok && objID == id {
			var incoming map[string]any
			if err := json.Unmarshal(item, &incoming); err != nil {
				return err
			}
			if _, hasID := incoming["id"]; !hasID {
				incoming["id"] = id
			}
			merged, err := json.Marshal(incoming)
			if err != nil {
				return err
			}
			arr[i] = merged
			found = true
			break
		}
	}

	if !found {
		var incoming map[string]any
		if err := json.Unmarshal(item, &incoming); err != nil {
			return err
		}
		if _, hasID := incoming["id"]; !hasID {
			incoming["id"] = id
		}
		withID, err := json.Marshal(incoming)
		if err != nil {
			return err
		}
		arr = append(arr, withID)
	}

	final, err := json.Marshal(arr)
	if err != nil {
		return err
	}

	now := s.now()
	_, err = tx.Exec(
		`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		key, string(final), now, source,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, final)
	return nil
}

// MergeByIdIfMatch is MergeById with an optimistic-concurrency precondition.
func (s *SQLiteStore) MergeByIdIfMatch(key, id string, patch json.RawMessage, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	for i, existing := range arr {
		var obj map[string]any
		if err := json.Unmarshal(existing, &obj); err != nil {
			continue
		}
		if objID, ok := obj["id"].(string); ok && objID == id {
			merged := jsonMergePatch(existing, patch)
			arr[i] = merged
			break
		}
	}

	final, err := json.Marshal(arr)
	if err != nil {
		return err
	}

	now := s.now()
	_, err = tx.Exec(
		`UPDATE data SET value = ?, updated_at = ?, updated_by = ? WHERE key = ?`,
		string(final), now, source, key,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, final)
	return nil
}

// DeleteByIdIfMatch is DeleteById with an optimistic-concurrency precondition.
func (s *SQLiteStore) DeleteByIdIfMatch(key, id, source, expectedUpdatedAt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.checkIfMatch(tx, key, expectedUpdatedAt); err != nil {
		return err
	}
	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	filtered := make([]json.RawMessage, 0, len(arr))
	for _, existing := range arr {
		var obj map[string]any
		if err := json.Unmarshal(existing, &obj); err != nil {
			filtered = append(filtered, existing)
			continue
		}
		if objID, ok := obj["id"].(string); ok && objID == id {
			continue
		}
		filtered = append(filtered, existing)
	}

	final, err := json.Marshal(filtered)
	if err != nil {
		return err
	}

	now := s.now()
	_, err = tx.Exec(
		`UPDATE data SET value = ?, updated_at = ?, updated_by = ? WHERE key = ?`,
		string(final), now, source, key,
	)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify(key, final)
	return nil
}
