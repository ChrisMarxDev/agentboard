package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DataEvent is emitted on every write for SSE broadcasting.
type DataEvent struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// Schema describes the inferred JSON shape of a value.
type Schema struct {
	Type       string            `json:"type"`
	Properties map[string]Schema `json:"properties,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
}

// DataStore is the interface for all data operations.
type DataStore interface {
	Set(key string, value json.RawMessage, source string) error
	Merge(key string, patch json.RawMessage, source string) error
	UpsertById(key, id string, item json.RawMessage, source string) error
	MergeById(key, id string, patch json.RawMessage, source string) error
	Append(key string, item json.RawMessage, source string) error
	Delete(key string, source string) error
	DeleteById(key, id string, source string) error

	Get(key string) (json.RawMessage, error)
	GetById(key, id string) (json.RawMessage, error)
	GetAll(prefix string, keys []string) (map[string]json.RawMessage, error)
	GetMeta(key string) (*DataMeta, error)
	InferSchema() (map[string]Schema, error)
	ListKeys() ([]string, error)

	Subscribe() <-chan DataEvent
	Close() error
}

// DataMeta holds metadata about a data key.
type DataMeta struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updated_at"`
	UpdatedBy string          `json:"updated_by"`
}

// SQLiteStore implements DataStore using SQLite.
type SQLiteStore struct {
	db          *sql.DB
	mu          sync.Mutex // serializes all writes
	subscribers []chan DataEvent
	subMu       sync.Mutex
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS data (
    key         TEXT NOT NULL PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    updated_by  TEXT NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS data_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    key         TEXT NOT NULL,
    value       TEXT NOT NULL,
    written_at  TEXT NOT NULL,
    written_by  TEXT NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_history_key ON data_history(key, written_at);
CREATE INDEX IF NOT EXISTS idx_history_written_at ON data_history(written_at);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;
`

// NewSQLiteStore creates a new SQLite-backed data store.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Run schema
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Set schema version if not exists
	_, err = db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('schema_version', '1')`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("set schema version: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`INSERT OR IGNORE INTO meta (key, value) VALUES ('created_at', ?)`, now)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("set created_at: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// DB returns the underlying connection pool so companion stores (e.g.
// internal/auth) can share the same SQLite file without opening a second
// handle. Callers MUST NOT close the returned *sql.DB; lifecycle belongs
// to SQLiteStore.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// archivePrevious records the current value in history before overwriting.
func (s *SQLiteStore) archivePrevious(tx *sql.Tx, key string) error {
	var value, updatedAt, updatedBy string
	err := tx.QueryRow(`SELECT value, updated_at, updated_by FROM data WHERE key = ?`, key).
		Scan(&value, &updatedAt, &updatedBy)
	if err == sql.ErrNoRows {
		return nil // nothing to archive
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO data_history (key, value, written_at, written_by) VALUES (?, ?, ?, ?)`,
		key, value, updatedAt, updatedBy,
	)
	return err
}

// notify sends a DataEvent to all subscribers.
func (s *SQLiteStore) notify(key string, value json.RawMessage) {
	evt := DataEvent{Key: key, Value: value}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- evt:
		default:
			// drop if subscriber is slow
		}
	}
}

func (s *SQLiteStore) Subscribe() <-chan DataEvent {
	ch := make(chan DataEvent, 64)
	s.subMu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.subMu.Unlock()
	return ch
}

func (s *SQLiteStore) Close() error {
	s.subMu.Lock()
	for _, ch := range s.subscribers {
		close(ch)
	}
	s.subscribers = nil
	s.subMu.Unlock()
	return s.db.Close()
}

func (s *SQLiteStore) Set(key string, value json.RawMessage, source string) error {
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

func (s *SQLiteStore) Get(key string) (json.RawMessage, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM data WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(value), nil
}

func (s *SQLiteStore) GetMeta(key string) (*DataMeta, error) {
	var m DataMeta
	var value string
	err := s.db.QueryRow(
		`SELECT key, value, updated_at, updated_by FROM data WHERE key = ?`, key,
	).Scan(&m.Key, &value, &m.UpdatedAt, &m.UpdatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Value = json.RawMessage(value)
	return &m, nil
}

func (s *SQLiteStore) GetAll(prefix string, keys []string) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage)

	if len(keys) > 0 {
		for _, k := range keys {
			val, err := s.Get(k)
			if err != nil {
				return nil, err
			}
			if val != nil {
				result[k] = val
			}
		}
		return result, nil
	}

	var rows *sql.Rows
	var err error
	if prefix != "" {
		rows, err = s.db.Query(`SELECT key, value FROM data WHERE key LIKE ? || '%'`, prefix)
	} else {
		rows, err = s.db.Query(`SELECT key, value FROM data`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = json.RawMessage(value)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) ListKeys() ([]string, error) {
	rows, err := s.db.Query(`SELECT key FROM data ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *SQLiteStore) Delete(key string, source string) error {
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

// PruneHistory removes history entries older than the given duration.
func (s *SQLiteStore) PruneHistory(ctx context.Context, retention time.Duration) error {
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `DELETE FROM data_history WHERE written_at < ?`, cutoff)
	return err
}
