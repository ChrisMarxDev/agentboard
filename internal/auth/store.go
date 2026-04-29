package auth

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Standard errors. Handlers translate these to HTTP status codes.
var (
	ErrNotFound        = errors.New("not found")
	ErrUsernameTaken   = errors.New("username already in use")
	ErrUserDeactivated = errors.New("user is deactivated")
	ErrTokenRevoked    = errors.New("token revoked")
	ErrImmutableField  = errors.New("username is immutable; use `agentboard admin rename-user` on the host")
)

// Kind enumerates the three user kinds. All auth via tokens.
//
//   - admin: manages users, invitations, teams, webhooks, page locks,
//     and any content. Unlocks /api/admin/*.
//   - member: normal human user. Reads/writes content. Manages *own*
//     tokens. Cannot manage other users or lock pages.
//   - bot: shared puppet. Any admin can mint/rotate/revoke its tokens.
//     Behaves like a member otherwise. Cannot create invitations or
//     page locks.
type Kind string

const (
	KindAdmin  Kind = "admin"
	KindMember Kind = "member"
	KindBot    Kind = "bot"
)

// ValidKinds returns the set of kind values that CreateUser accepts.
// Callers validating JSON input use this to reject unknown roles
// early (before the DB CHECK constraint does).
func ValidKinds() []Kind {
	return []Kind{KindAdmin, KindMember, KindBot}
}

// AccessMode controls rule evaluation for a user's tokens.
type AccessMode string

const (
	ModeAllowAll       AccessMode = "allow_all"
	ModeRestrictToList AccessMode = "restrict_to_list"
)

// User is one row in users. The username is the primary key — no separate
// id field. See AUTH.md for the reasoning.
type User struct {
	Username      string     `json:"username"`
	DisplayName   string     `json:"display_name,omitempty"`
	Kind          Kind       `json:"kind"`
	AvatarColor   string     `json:"avatar_color,omitempty"`
	AccessMode    AccessMode `json:"access_mode"`
	Rules         []Rule     `json:"rules"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedBy     string     `json:"created_by,omitempty"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`

	// Tokens, when populated by ListUsers(withTokens=true), holds every
	// token owned by this user — metadata only, never plaintext or hash.
	Tokens []UserToken `json:"tokens,omitempty"`
}

// UserToken is one row in user_tokens. Tokens rotate and a user can hold
// several, so tokens have their own uuid. The FK is users.username.
//
// CreatedBy captures the username that minted the token — matters for
// bot-kind users where multiple admins may share mint/rotate rights.
// Backfilled rows carry an empty string.
type UserToken struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	Label      string     `json:"label,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	CreatedBy  string     `json:"created_by,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// Store owns the users and user_tokens tables.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// ------------------------------------------------------------------
// Users
// ------------------------------------------------------------------

// CreateUserParams bundles inputs for CreateUser.
type CreateUserParams struct {
	Username    string
	DisplayName string
	Kind        Kind
	AccessMode  AccessMode
	Rules       []Rule
	CreatedBy   string
}

// CreateUser inserts a new user. Validates username format and uniqueness.
// Does NOT mint a token — callers use CreateToken afterwards.
func (s *Store) CreateUser(p CreateUserParams) (*User, error) {
	p.Username = strings.ToLower(strings.TrimSpace(p.Username))
	if err := ValidateUsername(p.Username); err != nil {
		return nil, err
	}
	if p.AccessMode == "" {
		p.AccessMode = ModeAllowAll
	}
	rulesJSON, err := json.Marshal(ensureRules(p.Rules))
	if err != nil {
		return nil, fmt.Errorf("marshal rules: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.db.Exec(
		`INSERT INTO users (username, display_name, kind, avatar_color, access_mode, rules_json, created_at, created_by)
		 VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		p.Username, p.DisplayName, string(p.Kind), avatarColorForUsername(p.Username),
		string(p.AccessMode), string(rulesJSON), now.Unix(), p.CreatedBy,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}
	return s.GetUser(p.Username)
}

// GetUser returns a user by username (case-insensitive).
func (s *Store) GetUser(username string) (*User, error) {
	return s.queryUser(`WHERE username = ? COLLATE NOCASE`, strings.ToLower(username))
}

// ListUsers returns every user. When withTokens is true, each User's
// Tokens field is populated.
func (s *Store) ListUsers(withTokens bool) ([]*User, error) {
	rows, err := s.db.Query(`SELECT username, display_name, kind, avatar_color,
	                                access_mode, rules_json, created_at, created_by, deactivated_at
	                         FROM users
	                         ORDER BY kind DESC, username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if withTokens {
		for _, u := range users {
			tokens, err := s.ListTokensForUser(u.Username)
			if err != nil {
				return nil, err
			}
			u.Tokens = tokens
		}
	}
	return users, nil
}

// HasAnyUser reports whether at least one active user exists. Used to
// decide whether the server runs in "open loopback" mode at startup.
func (s *Store) HasAnyUser() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE deactivated_at IS NULL`).Scan(&n)
	return n > 0, err
}

// UpdateUserParams lists the fields normal edits can change. Notably
// username is not in here — it's immutable via this path.
type UpdateUserParams struct {
	DisplayName *string
	AccessMode  *AccessMode
	Rules       *[]Rule
}

// UpdateUser updates mutable user fields. To change the username, use the
// RenameUser method (exposed only via CLI).
func (s *Store) UpdateUser(username string, p UpdateUserParams) (*User, error) {
	existing, err := s.GetUser(username)
	if err != nil {
		return nil, err
	}
	if p.DisplayName != nil {
		existing.DisplayName = *p.DisplayName
	}
	if p.AccessMode != nil {
		existing.AccessMode = *p.AccessMode
	}
	if p.Rules != nil {
		existing.Rules = *p.Rules
	}
	rulesJSON, err := json.Marshal(ensureRules(existing.Rules))
	if err != nil {
		return nil, fmt.Errorf("marshal rules: %w", err)
	}
	_, err = s.db.Exec(
		`UPDATE users SET display_name = NULLIF(?, ''), access_mode = ?, rules_json = ?
		 WHERE username = ? COLLATE NOCASE`,
		existing.DisplayName, string(existing.AccessMode), string(rulesJSON), existing.Username,
	)
	if err != nil {
		return nil, err
	}
	return s.GetUser(existing.Username)
}

// Deactivate marks a user as deactivated and revokes all of their tokens.
// The username is reserved forever; it cannot be reused by a new user.
func (s *Store) Deactivate(username string) error {
	now := time.Now().UTC().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE users SET deactivated_at = ? WHERE username = ? COLLATE NOCASE AND deactivated_at IS NULL`, now, username)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`UPDATE user_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE username = ? COLLATE NOCASE`, now, username); err != nil {
		return err
	}
	return tx.Commit()
}

// RenameUser is the escape hatch for the typo-on-create case. Transactional.
// Updates users PK, user_tokens.username, and returns a count of how many
// rows were updated across tables. Callers (CLI) print that count so the
// operator knows whether follow-up work is needed on free-text content.
//
// Content-side rewrites (MDX pages, data values containing `@old`) are NOT
// performed — this is deliberate because free-text rewrites across a live
// system are unsafe. The CLI warns.
type RenameStats struct {
	UsersUpdated  int
	TokensUpdated int
}

// RenameUser replaces the username on a user plus every token that
// references it. Fails if the new username is invalid or taken, or if the
// old username doesn't exist.
func (s *Store) RenameUser(oldUsername, newUsername string) (*RenameStats, error) {
	oldUsername = strings.ToLower(strings.TrimSpace(oldUsername))
	newUsername = strings.ToLower(strings.TrimSpace(newUsername))
	if err := ValidateUsername(newUsername); err != nil {
		return nil, err
	}
	if oldUsername == newUsername {
		return &RenameStats{}, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE users SET username = ? WHERE username = ? COLLATE NOCASE`, newUsername, oldUsername)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("update users: %w", err)
	}
	usersUpdated, _ := res.RowsAffected()
	if usersUpdated == 0 {
		return nil, ErrNotFound
	}
	res2, err := tx.Exec(`UPDATE user_tokens SET username = ? WHERE username = ? COLLATE NOCASE`, newUsername, oldUsername)
	if err != nil {
		return nil, fmt.Errorf("update user_tokens: %w", err)
	}
	tokensUpdated, _ := res2.RowsAffected()

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RenameStats{
		UsersUpdated:  int(usersUpdated),
		TokensUpdated: int(tokensUpdated),
	}, nil
}

// ResolveUsernames batches a lookup. Unknown usernames are dropped
// silently. Used by @mention rendering and by assignee validators.
func (s *Store) ResolveUsernames(usernames []string) ([]*User, error) {
	if len(usernames) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(usernames)), ",")
	args := make([]any, len(usernames))
	for i, u := range usernames {
		args[i] = strings.ToLower(strings.TrimSpace(u))
	}
	query := `SELECT username, display_name, kind, avatar_color,
	                 access_mode, rules_json, created_at, created_by, deactivated_at
	          FROM users WHERE username IN (` + placeholders + `) COLLATE NOCASE`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------
// Tokens
// ------------------------------------------------------------------

// CreateTokenParams bundles inputs for CreateToken.
type CreateTokenParams struct {
	Username  string
	TokenHash string
	Label     string
	CreatedBy string // optional — the username that minted (for audit)
}

// CreateToken inserts a token for a user. Returns metadata; the plaintext
// token lives only in whatever the caller generated and shows the operator
// once.
func (s *Store) CreateToken(p CreateTokenParams) (*UserToken, error) {
	if p.TokenHash == "" {
		return nil, fmt.Errorf("token hash required")
	}
	p.Username = strings.ToLower(strings.TrimSpace(p.Username))
	now := time.Now().UTC()
	tokenID := uuid.NewString()
	_, err := s.db.Exec(
		`INSERT INTO user_tokens (id, username, token_hash, label, created_at, created_by)
		 VALUES (?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''))`,
		tokenID, p.Username, p.TokenHash, p.Label, now.Unix(), p.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("insert token: %w", err)
	}
	return s.GetToken(tokenID)
}

// GetToken returns one token row by its uuid.
func (s *Store) GetToken(id string) (*UserToken, error) {
	row := s.db.QueryRow(
		`SELECT id, username, label, created_at, created_by, last_used_at, revoked_at
		 FROM user_tokens WHERE id = ?`, id,
	)
	return scanToken(row)
}

// ListTokensForUser returns every token owned by a user (active + revoked).
func (s *Store) ListTokensForUser(username string) ([]UserToken, error) {
	rows, err := s.db.Query(
		`SELECT id, username, label, created_at, created_by, last_used_at, revoked_at
		 FROM user_tokens WHERE username = ? COLLATE NOCASE ORDER BY created_at ASC`,
		strings.ToLower(username),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// RotateToken replaces a token's hash in place and clears revoked_at so a
// previously-revoked row can be "un-revoked" by rotation. Label + user
// association are preserved.
func (s *Store) RotateToken(tokenID, newTokenHash string) error {
	res, err := s.db.Exec(
		`UPDATE user_tokens SET token_hash = ?, revoked_at = NULL WHERE id = ?`,
		newTokenHash, tokenID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeToken soft-deletes a single token.
func (s *Store) RevokeToken(tokenID string) error {
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`UPDATE user_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, tokenID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResolveToken takes a token hash and returns (user, token) for the
// request. Errors:
//   - ErrNotFound: no matching token
//   - ErrTokenRevoked: token row is revoked
//   - ErrUserDeactivated: user row is deactivated (still returns the
//     partial structs so middleware can log)
func (s *Store) ResolveToken(tokenHash string) (*User, *UserToken, error) {
	row := s.db.QueryRow(
		`SELECT u.username, u.display_name, u.kind, u.avatar_color,
		        u.access_mode, u.rules_json, u.created_at, u.created_by, u.deactivated_at,
		        t.id, t.username, t.label, t.created_at, t.created_by, t.last_used_at, t.revoked_at
		 FROM user_tokens t JOIN users u ON u.username = t.username COLLATE NOCASE
		 WHERE t.token_hash = ?`, tokenHash,
	)
	var (
		u User
		t UserToken

		displayName sql.NullString
		avatarColor sql.NullString
		createdBy   sql.NullString
		userCreated int64
		deactivated sql.NullInt64
		rulesJSON   string

		tokenLabel     sql.NullString
		tokenCreated   int64
		tokenCreatedBy sql.NullString
		tokenLastUsed  sql.NullInt64
		tokenRevoked   sql.NullInt64
	)
	err := row.Scan(
		&u.Username, &displayName, &u.Kind, &avatarColor,
		&u.AccessMode, &rulesJSON, &userCreated, &createdBy, &deactivated,
		&t.ID, &t.Username, &tokenLabel, &tokenCreated, &tokenCreatedBy, &tokenLastUsed, &tokenRevoked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	u.DisplayName = displayName.String
	u.AvatarColor = avatarColor.String
	u.CreatedAt = time.Unix(userCreated, 0).UTC()
	u.CreatedBy = createdBy.String
	if deactivated.Valid {
		tt := time.Unix(deactivated.Int64, 0).UTC()
		u.DeactivatedAt = &tt
	}
	u.Rules = ensureRules(nil)
	if rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &u.Rules); err != nil {
			return nil, nil, fmt.Errorf("unmarshal rules: %w", err)
		}
	}
	t.Label = tokenLabel.String
	t.CreatedAt = time.Unix(tokenCreated, 0).UTC()
	t.CreatedBy = tokenCreatedBy.String
	if tokenLastUsed.Valid {
		tt := time.Unix(tokenLastUsed.Int64, 0).UTC()
		t.LastUsedAt = &tt
	}
	if tokenRevoked.Valid {
		tt := time.Unix(tokenRevoked.Int64, 0).UTC()
		t.RevokedAt = &tt
		return &u, &t, ErrTokenRevoked
	}
	if u.DeactivatedAt != nil {
		return &u, &t, ErrUserDeactivated
	}
	return &u, &t, nil
}

// TouchTokenLastUsed bumps last_used_at. Called opportunistically by the
// middleware, coalesced to one write per minute per token.
func (s *Store) TouchTokenLastUsed(tokenID string) error {
	_, err := s.db.Exec(
		`UPDATE user_tokens SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), tokenID,
	)
	return err
}

// ------------------------------------------------------------------
// scanners
// ------------------------------------------------------------------

func scanUser(r rowScanner) (*User, error) {
	var u User
	var displayName, avatarColor, createdBy sql.NullString
	var createdAt int64
	var deactivated sql.NullInt64
	var rulesJSON string
	err := r.Scan(&u.Username, &displayName, &u.Kind, &avatarColor,
		&u.AccessMode, &rulesJSON, &createdAt, &createdBy, &deactivated)
	if err != nil {
		return nil, err
	}
	u.DisplayName = displayName.String
	u.AvatarColor = avatarColor.String
	u.CreatedBy = createdBy.String
	u.CreatedAt = time.Unix(createdAt, 0).UTC()
	if deactivated.Valid {
		t := time.Unix(deactivated.Int64, 0).UTC()
		u.DeactivatedAt = &t
	}
	u.Rules = ensureRules(nil)
	if rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &u.Rules); err != nil {
			return nil, fmt.Errorf("unmarshal rules for %s: %w", u.Username, err)
		}
	}
	return &u, nil
}

func scanToken(r rowScanner) (*UserToken, error) {
	var t UserToken
	var label, createdBy sql.NullString
	var created int64
	var lastUsed, revoked sql.NullInt64
	err := r.Scan(&t.ID, &t.Username, &label, &created, &createdBy, &lastUsed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	t.Label = label.String
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.CreatedBy = createdBy.String
	if lastUsed.Valid {
		tt := time.Unix(lastUsed.Int64, 0).UTC()
		t.LastUsedAt = &tt
	}
	if revoked.Valid {
		tt := time.Unix(revoked.Int64, 0).UTC()
		t.RevokedAt = &tt
	}
	return &t, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) queryUser(whereClause string, args ...any) (*User, error) {
	query := `SELECT username, display_name, kind, avatar_color,
	                 access_mode, rules_json, created_at, created_by, deactivated_at
	          FROM users ` + whereClause
	row := s.db.QueryRow(query, args...)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "UNIQUE constraint failed", "constraint failed: UNIQUE")
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
