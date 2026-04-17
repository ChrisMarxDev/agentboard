package data

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Merge performs a deep merge of patch into the existing value at key.
// Uses RFC 7396 JSON Merge Patch semantics.
func (s *SQLiteStore) Merge(key string, patch json.RawMessage, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	// Try to use json_patch if row exists
	now := s.now()
	var existing string
	err = tx.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&existing)
	if err == sql.ErrNoRows {
		// No existing value, just insert
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

	// Read back the final value for notification
	var finalValue string
	_ = tx.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&finalValue)

	if err := tx.Commit(); err != nil {
		return err
	}

	s.notify(key, json.RawMessage(finalValue))
	return nil
}

// UpsertById inserts or updates an item in a collection (JSON array) by its id field.
func (s *SQLiteStore) UpsertById(key, id string, item json.RawMessage, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	// Ensure the item has the correct id
	var itemMap map[string]interface{}
	if err := json.Unmarshal(item, &itemMap); err != nil {
		return fmt.Errorf("item must be a JSON object: %w", err)
	}
	itemMap["id"] = id

	// Find and replace, or append
	found := false
	for i, existing := range arr {
		var obj map[string]interface{}
		if err := json.Unmarshal(existing, &obj); err != nil {
			continue
		}
		if fmt.Sprint(obj["id"]) == id {
			updated, _ := json.Marshal(itemMap)
			arr[i] = updated
			found = true
			break
		}
	}
	if !found {
		updated, _ := json.Marshal(itemMap)
		arr = append(arr, updated)
	}

	finalValue, _ := json.Marshal(arr)
	now := s.now()
	_, err = tx.Exec(
		`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		key, string(finalValue), now, source,
	)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.notify(key, finalValue)
	return nil
}

// MergeById deep merges a patch into a specific item in a collection.
func (s *SQLiteStore) MergeById(key, id string, patch json.RawMessage, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	found := false
	for i, existing := range arr {
		var obj map[string]interface{}
		if err := json.Unmarshal(existing, &obj); err != nil {
			continue
		}
		if fmt.Sprint(obj["id"]) == id {
			arr[i] = jsonMergePatch(existing, patch)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("item with id %q not found in collection %q", id, key)
	}

	finalValue, _ := json.Marshal(arr)
	now := s.now()
	_, err = tx.Exec(
		`UPDATE data SET value = ?, updated_at = ?, updated_by = ? WHERE key = ?`,
		string(finalValue), now, source, key,
	)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.notify(key, finalValue)
	return nil
}

// Append adds an item to a JSON array at key. Creates the array if key doesn't exist.
func (s *SQLiteStore) Append(key string, item json.RawMessage, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	arr = append(arr, item)

	finalValue, _ := json.Marshal(arr)
	now := s.now()
	_, err = tx.Exec(
		`INSERT INTO data (key, value, updated_at, updated_by) VALUES (?, ?, ?, ?)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by`,
		key, string(finalValue), now, source,
	)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.notify(key, finalValue)
	return nil
}

// DeleteById removes an item from a collection by its id field.
func (s *SQLiteStore) DeleteById(key, id string, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.archivePrevious(tx, key); err != nil {
		return err
	}

	arr, err := s.loadArray(tx, key)
	if err != nil {
		return err
	}

	newArr := make([]json.RawMessage, 0, len(arr))
	for _, existing := range arr {
		var obj map[string]interface{}
		if err := json.Unmarshal(existing, &obj); err != nil {
			newArr = append(newArr, existing)
			continue
		}
		if fmt.Sprint(obj["id"]) != id {
			newArr = append(newArr, existing)
		}
	}

	finalValue, _ := json.Marshal(newArr)
	now := s.now()
	_, err = tx.Exec(
		`UPDATE data SET value = ?, updated_at = ?, updated_by = ? WHERE key = ?`,
		string(finalValue), now, source, key,
	)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.notify(key, finalValue)
	return nil
}

// GetById returns a specific item from a collection by its id field.
func (s *SQLiteStore) GetById(key, id string) (json.RawMessage, error) {
	value, err := s.Get(key)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(value, &arr); err != nil {
		return nil, fmt.Errorf("value at %q is not an array", key)
	}

	for _, item := range arr {
		var obj map[string]interface{}
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		if fmt.Sprint(obj["id"]) == id {
			return item, nil
		}
	}
	return nil, nil
}

// loadArray loads the value at key as a JSON array, or returns an empty array.
func (s *SQLiteStore) loadArray(tx *sql.Tx, key string) ([]json.RawMessage, error) {
	var value string
	err := tx.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return []json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}

	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(value), &arr); err != nil {
		return nil, fmt.Errorf("value at %q is not an array: %w", key, err)
	}
	return arr, nil
}

// jsonMergePatch implements RFC 7396 JSON Merge Patch.
func jsonMergePatch(original, patch []byte) []byte {
	var origMap map[string]interface{}
	var patchMap map[string]interface{}

	if err := json.Unmarshal(patch, &patchMap); err != nil {
		// If patch is not an object, it replaces entirely
		return patch
	}

	if err := json.Unmarshal(original, &origMap); err != nil {
		// If original is not an object, patch replaces entirely
		result, _ := json.Marshal(patchMap)
		return result
	}

	for k, v := range patchMap {
		if v == nil {
			delete(origMap, k)
		} else {
			origVal, exists := origMap[k]
			if exists {
				origBytes, _ := json.Marshal(origVal)
				patchBytes, _ := json.Marshal(v)
				merged := jsonMergePatch(origBytes, patchBytes)
				var mergedVal interface{}
				json.Unmarshal(merged, &mergedVal)
				origMap[k] = mergedVal
			} else {
				origMap[k] = v
			}
		}
	}

	result, _ := json.Marshal(origMap)
	return result
}
