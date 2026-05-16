package gitserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Event is a record on the in-process event hub. Post-pivot it feeds
// the SSE broadcaster (so browser tabs show a non-modal "reload" toast
// after a push) and the FTS5 re-index hook. The MCP event-bus tools
// that previously consumed these were cut with the pivot — agents
// re-pull on a cadence instead.
type Event struct {
	ID        int64           `json:"id"`
	Workspace string          `json:"workspace"`
	Type      string          `json:"type"` // "push" | "conflict" | "merge" | "mention" | ...
	Actor     string          `json:"actor,omitempty"`
	At        int64           `json:"at"` // unix seconds
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (s *Store) eventsMigrate() error {
	const schemaSQL = `
CREATE TABLE IF NOT EXISTS git_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace   TEXT NOT NULL,
    type        TEXT NOT NULL,
    actor       TEXT,
    at          INTEGER NOT NULL,
    payload     TEXT NOT NULL DEFAULT '{}'
) STRICT;

CREATE INDEX IF NOT EXISTS idx_git_events_workspace_id ON git_events(workspace, id);
CREATE INDEX IF NOT EXISTS idx_git_events_id          ON git_events(id);
`
	_, err := s.db.Exec(schemaSQL)
	return err
}

// AppendEvent records one event. Returns the assigned row id.
// Errors are non-fatal for callers: events are best-effort telemetry,
// not a guarantee.
func (s *Store) AppendEvent(ctx context.Context, e Event) (int64, error) {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO git_events (workspace, type, actor, at, payload)
		 VALUES (?, ?, ?, ?, ?)`,
		e.Workspace, e.Type, e.Actor, e.At, string(payload))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	// Cheap retention: keep the last 1000 rows per workspace. The
	// table grows unbounded otherwise — subscribe consumers should
	// rarely fall more than 1000 events behind.
	_, _ = s.db.ExecContext(ctx,
		`DELETE FROM git_events
		 WHERE id IN (
		   SELECT id FROM git_events
		   WHERE workspace = ?
		   ORDER BY id DESC
		   LIMIT -1 OFFSET 1000
		 )`, e.Workspace)
	return id, nil
}

// ListEvents returns events strictly newer than `sinceID` for the
// given workspace (empty = all workspaces) filtered to the given
// types (empty = all types). Capped at 200 rows per call.
func (s *Store) ListEvents(ctx context.Context, workspace string, sinceID int64, types []string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `SELECT id, workspace, type, IFNULL(actor,''), at, payload FROM git_events WHERE id > ?`
	args := []any{sinceID}
	if workspace != "" {
		query += ` AND workspace = ?`
		args = append(args, workspace)
	}
	if len(types) > 0 {
		placeholders := ""
		for i, t := range types {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, t)
		}
		query += ` AND type IN (` + placeholders + `)`
	}
	query += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var payload string
		if err := rows.Scan(&e.ID, &e.Workspace, &e.Type, &e.Actor, &e.At, &payload); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// CurrentEventCursor returns the highest event id known to the store.
// Subscribe callers use it on their first call to skip historical
// events; subsequent calls pass back the highest id they've seen.
func (s *Store) CurrentEventCursor(ctx context.Context) (int64, error) {
	row := s.db.QueryRowContext(ctx, `SELECT IFNULL(MAX(id), 0) FROM git_events`)
	var max int64
	if err := row.Scan(&max); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("read max event id: %w", err)
	}
	return max, nil
}
