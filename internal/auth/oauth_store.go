package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Token-format prefixes. Distinct from PATs (`ab_`) so an operator
// reading a log line can tell at a glance which credential pipeline
// minted the value.
const (
	OAuthClientIDPrefix     = "mcpc_" // registered client id (public, not secret)
	OAuthClientSecretPrefix = "mcs_"  // confidential-client secret
	OAuthCodePrefix         = "oac_"  // authorization code (single-use, ~5 min TTL)
	OAuthAccessPrefix       = "oat_"  // access token (Bearer)
	OAuthRefreshPrefix      = "ort_"  // refresh token (single-use, rotated per OAuth 2.1)
)

// Default lifetimes. Tunable later if we need to.
const (
	OAuthCodeTTL    = 5 * time.Minute
	OAuthAccessTTL  = time.Hour
	OAuthRefreshTTL = 30 * 24 * time.Hour
)

// OAuth-specific error sentinels.
var (
	ErrOAuthClientNotFound = errors.New("oauth client not found")
	ErrOAuthCodeInvalid    = errors.New("oauth authorization code invalid or expired")
	ErrOAuthTokenInvalid   = errors.New("oauth token invalid, expired, or revoked")
	ErrOAuthAudience       = errors.New("oauth token audience does not match resource")
)

// OAuthClient is one row in oauth_clients. ClientSecretPlaintext is
// only populated by CreateOAuthClient on the way out — never read back.
type OAuthClient struct {
	ClientID                string    `json:"client_id"`
	ClientName              string    `json:"client_name"`
	RedirectURIs            []string  `json:"redirect_uris"`
	GrantTypes              []string  `json:"grant_types"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method"`
	Scope                   string    `json:"scope"`
	CreatedAt               time.Time `json:"client_id_issued_at"`
	HasSecret               bool      `json:"-"`

	// ClientSecretPlaintext is only set by CreateOAuthClient; it is the
	// only moment the secret exists in cleartext. Subsequent reads from
	// the DB never repopulate it.
	ClientSecretPlaintext string `json:"-"`
}

// OAuthAccessRecord is one row in oauth_tokens — what the middleware
// resolves a presented Bearer to when it isn't a PAT.
type OAuthAccessRecord struct {
	ID                string
	Username          string
	ClientID          string
	Scope             string
	Audience          string
	AccessExpiresAt   time.Time
	RefreshExpiresAt  *time.Time
	HasRefresh        bool
	CreatedAt         time.Time
	RevokedAt         *time.Time
	LastUsedAt        *time.Time
}

// OAuthCodeRecord is the parsed authorization code payload.
type OAuthCodeRecord struct {
	ClientID            string
	Username            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	Audience            string
	ExpiresAt           time.Time
}

// CreateOAuthClientParams bundles the inputs DCR (RFC 7591) accepts.
type CreateOAuthClientParams struct {
	ClientName              string
	RedirectURIs            []string
	GrantTypes              []string
	TokenEndpointAuthMethod string
	Scope                   string
	CreatedBy               string // empty for DCR-anonymous registrations
}

// CreateOAuthClient registers a new MCP client. For confidential clients
// (auth_method != "none") a secret is generated, returned once via
// ClientSecretPlaintext, and stored as a sha256 hash.
func (s *Store) CreateOAuthClient(p CreateOAuthClientParams) (*OAuthClient, error) {
	if p.ClientName == "" {
		p.ClientName = "Unnamed MCP client"
	}
	if len(p.RedirectURIs) == 0 {
		return nil, fmt.Errorf("redirect_uris is required")
	}
	for _, u := range p.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return nil, fmt.Errorf("redirect_uri %q: %w", u, err)
		}
	}
	if len(p.GrantTypes) == 0 {
		p.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if p.TokenEndpointAuthMethod == "" {
		p.TokenEndpointAuthMethod = "none"
	}
	switch p.TokenEndpointAuthMethod {
	case "none", "client_secret_basic", "client_secret_post":
	default:
		return nil, fmt.Errorf("unsupported token_endpoint_auth_method %q", p.TokenEndpointAuthMethod)
	}
	if p.Scope == "" {
		p.Scope = "mcp"
	}

	clientID, err := generateOAuthID(OAuthClientIDPrefix, 16)
	if err != nil {
		return nil, err
	}
	var secretHash sql.NullString
	var secretPlain string
	if p.TokenEndpointAuthMethod != "none" {
		secretPlain, err = generateOAuthSecret(OAuthClientSecretPrefix)
		if err != nil {
			return nil, err
		}
		secretHash = sql.NullString{String: HashToken(secretPlain), Valid: true}
	}
	redirectsJSON, _ := json.Marshal(p.RedirectURIs)
	grantsJSON, _ := json.Marshal(p.GrantTypes)
	now := time.Now().UTC()

	_, err = s.db.Exec(
		`INSERT INTO oauth_clients
		 (client_id, client_secret_hash, client_name, redirect_uris_json,
		  grant_types_json, token_endpoint_auth_method, scope, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
		clientID, secretHash, p.ClientName, string(redirectsJSON),
		string(grantsJSON), p.TokenEndpointAuthMethod, p.Scope, now.Unix(), p.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("insert oauth_client: %w", err)
	}

	c := &OAuthClient{
		ClientID:                clientID,
		ClientName:              p.ClientName,
		RedirectURIs:            p.RedirectURIs,
		GrantTypes:              p.GrantTypes,
		TokenEndpointAuthMethod: p.TokenEndpointAuthMethod,
		Scope:                   p.Scope,
		CreatedAt:               now,
		HasSecret:               secretPlain != "",
		ClientSecretPlaintext:   secretPlain,
	}
	return c, nil
}

// GetOAuthClient looks up a registered client by id.
func (s *Store) GetOAuthClient(clientID string) (*OAuthClient, error) {
	row := s.db.QueryRow(
		`SELECT client_id, client_secret_hash, client_name, redirect_uris_json,
		        grant_types_json, token_endpoint_auth_method, scope, created_at
		 FROM oauth_clients WHERE client_id = ?`, clientID,
	)
	var (
		c           OAuthClient
		secretHash  sql.NullString
		redirectsJS string
		grantsJS    string
		createdAt   int64
	)
	err := row.Scan(&c.ClientID, &secretHash, &c.ClientName, &redirectsJS,
		&grantsJS, &c.TokenEndpointAuthMethod, &c.Scope, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthClientNotFound
	}
	if err != nil {
		return nil, err
	}
	c.HasSecret = secretHash.Valid
	if err := json.Unmarshal([]byte(redirectsJS), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode redirect_uris: %w", err)
	}
	if err := json.Unmarshal([]byte(grantsJS), &c.GrantTypes); err != nil {
		return nil, fmt.Errorf("decode grant_types: %w", err)
	}
	c.CreatedAt = time.Unix(createdAt, 0).UTC()
	return &c, nil
}

// VerifyClientSecret returns true if `presented` matches the stored
// hash for the given client. Constant-time comparison.
func (s *Store) VerifyClientSecret(clientID, presented string) bool {
	var h sql.NullString
	err := s.db.QueryRow(
		`SELECT client_secret_hash FROM oauth_clients WHERE client_id = ?`, clientID,
	).Scan(&h)
	if err != nil || !h.Valid {
		return false
	}
	return TokensEqual(h.String, HashToken(presented))
}

// CreateAuthCode generates and stores a new authorization code, returning
// the plaintext (URL-safe). Codes are single-use and short-lived.
func (s *Store) CreateAuthCode(clientID, username, redirectURI, codeChallenge, codeChallengeMethod, scope, audience string) (string, error) {
	plain, err := generateOAuthSecret(OAuthCodePrefix)
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(
		`INSERT INTO oauth_codes
		 (code_hash, client_id, username, redirect_uri, code_challenge,
		  code_challenge_method, scope, audience, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		HashToken(plain), clientID, strings.ToLower(username), redirectURI,
		codeChallenge, codeChallengeMethod, scope, audience,
		now.Add(OAuthCodeTTL).Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("insert oauth_code: %w", err)
	}
	return plain, nil
}

// ConsumeAuthCode atomically marks an authorization code as used and
// returns its parsed contents. Codes that don't exist, are expired, or
// were already used yield ErrOAuthCodeInvalid — distinguishing those
// cases would only help an attacker.
func (s *Store) ConsumeAuthCode(plain string) (*OAuthCodeRecord, error) {
	hash := HashToken(plain)
	now := time.Now().UTC().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRow(
		`SELECT client_id, username, redirect_uri, code_challenge,
		        code_challenge_method, scope, audience, expires_at, used_at
		 FROM oauth_codes WHERE code_hash = ?`, hash,
	)
	var (
		rec       OAuthCodeRecord
		expiresAt int64
		usedAt    sql.NullInt64
	)
	err = row.Scan(&rec.ClientID, &rec.Username, &rec.RedirectURI, &rec.CodeChallenge,
		&rec.CodeChallengeMethod, &rec.Scope, &rec.Audience, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	if usedAt.Valid || expiresAt < now {
		return nil, ErrOAuthCodeInvalid
	}
	if _, err := tx.Exec(
		`UPDATE oauth_codes SET used_at = ? WHERE code_hash = ? AND used_at IS NULL`,
		now, hash,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	rec.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return &rec, nil
}

// CreateAccessTokenParams bundles inputs to mint an access (and optional
// refresh) token after a successful authorize → token exchange.
type CreateAccessTokenParams struct {
	ClientID    string
	Username    string
	Scope       string
	Audience    string
	WithRefresh bool
}

// AccessTokenIssued is the plaintext payload returned to the client.
type AccessTokenIssued struct {
	ID                string
	AccessToken       string
	RefreshToken      string // empty if WithRefresh=false
	AccessExpiresIn   int    // seconds
	RefreshExpiresIn  int    // seconds; 0 if no refresh
	Scope             string
	Audience          string
}

// CreateAccessToken mints and stores an access token (and refresh, if
// requested), returning plaintexts to the caller exactly once.
func (s *Store) CreateAccessToken(p CreateAccessTokenParams) (*AccessTokenIssued, error) {
	access, err := generateOAuthSecret(OAuthAccessPrefix)
	if err != nil {
		return nil, err
	}
	var refreshHash sql.NullString
	var refreshPlain string
	var refreshExpiresAt sql.NullInt64
	if p.WithRefresh {
		refreshPlain, err = generateOAuthSecret(OAuthRefreshPrefix)
		if err != nil {
			return nil, err
		}
		refreshHash = sql.NullString{String: HashToken(refreshPlain), Valid: true}
		refreshExpiresAt = sql.NullInt64{Int64: time.Now().Add(OAuthRefreshTTL).Unix(), Valid: true}
	}
	id := uuid.NewString()
	now := time.Now().UTC()
	accessExpiresAt := now.Add(OAuthAccessTTL)

	_, err = s.db.Exec(
		`INSERT INTO oauth_tokens
		 (id, access_token_hash, refresh_token_hash, client_id, username,
		  scope, audience, access_expires_at, refresh_expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, HashToken(access), refreshHash, p.ClientID, strings.ToLower(p.Username),
		p.Scope, p.Audience, accessExpiresAt.Unix(), refreshExpiresAt, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert oauth_token: %w", err)
	}
	out := &AccessTokenIssued{
		ID:              id,
		AccessToken:     access,
		AccessExpiresIn: int(OAuthAccessTTL.Seconds()),
		Scope:           p.Scope,
		Audience:        p.Audience,
	}
	if p.WithRefresh {
		out.RefreshToken = refreshPlain
		out.RefreshExpiresIn = int(OAuthRefreshTTL.Seconds())
	}
	return out, nil
}

// ResolveAccessToken takes a sha256(access_token) and returns the
// matching record + user. Errors:
//   - ErrOAuthTokenInvalid: not found / expired / revoked
//   - ErrUserDeactivated: token row OK but the user has been deactivated
func (s *Store) ResolveAccessToken(tokenHash string) (*OAuthAccessRecord, *User, error) {
	row := s.db.QueryRow(
		`SELECT t.id, t.username, t.client_id, t.scope, t.audience,
		        t.access_expires_at, t.refresh_expires_at, t.refresh_token_hash IS NOT NULL,
		        t.created_at, t.revoked_at, t.last_used_at,
		        u.username, u.display_name, u.kind, u.avatar_color,
		        u.access_mode, u.rules_json, u.created_at, u.created_by, u.deactivated_at
		 FROM oauth_tokens t JOIN users u ON u.username = t.username COLLATE NOCASE
		 WHERE t.access_token_hash = ?`, tokenHash,
	)
	var (
		rec               OAuthAccessRecord
		accessExp, created int64
		refreshExp        sql.NullInt64
		hasRefresh        bool
		revokedAt         sql.NullInt64
		lastUsedAt        sql.NullInt64

		u           User
		displayName sql.NullString
		avatarColor sql.NullString
		userCreated int64
		createdBy   sql.NullString
		deactivated sql.NullInt64
		rulesJSON   string
	)
	err := row.Scan(
		&rec.ID, &rec.Username, &rec.ClientID, &rec.Scope, &rec.Audience,
		&accessExp, &refreshExp, &hasRefresh, &created, &revokedAt, &lastUsedAt,
		&u.Username, &displayName, &u.Kind, &avatarColor,
		&u.AccessMode, &rulesJSON, &userCreated, &createdBy, &deactivated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrOAuthTokenInvalid
	}
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC().Unix()
	if revokedAt.Valid || accessExp < now {
		return nil, nil, ErrOAuthTokenInvalid
	}
	rec.AccessExpiresAt = time.Unix(accessExp, 0).UTC()
	if refreshExp.Valid {
		t := time.Unix(refreshExp.Int64, 0).UTC()
		rec.RefreshExpiresAt = &t
	}
	rec.HasRefresh = hasRefresh
	rec.CreatedAt = time.Unix(created, 0).UTC()
	if revokedAt.Valid {
		t := time.Unix(revokedAt.Int64, 0).UTC()
		rec.RevokedAt = &t
	}
	if lastUsedAt.Valid {
		t := time.Unix(lastUsedAt.Int64, 0).UTC()
		rec.LastUsedAt = &t
	}

	u.DisplayName = displayName.String
	u.AvatarColor = avatarColor.String
	u.CreatedAt = time.Unix(userCreated, 0).UTC()
	u.CreatedBy = createdBy.String
	if deactivated.Valid {
		t := time.Unix(deactivated.Int64, 0).UTC()
		u.DeactivatedAt = &t
	}
	u.Rules = ensureRules(nil)
	if rulesJSON != "" {
		if err := json.Unmarshal([]byte(rulesJSON), &u.Rules); err != nil {
			return nil, nil, fmt.Errorf("unmarshal rules: %w", err)
		}
	}
	if u.DeactivatedAt != nil {
		return &rec, &u, ErrUserDeactivated
	}
	return &rec, &u, nil
}

// RotateRefreshToken atomically rotates a refresh token: revokes the
// old token row and mints a new (access, refresh) pair carrying the
// same client/user/scope/audience. Per OAuth 2.1 §4.3.1.
func (s *Store) RotateRefreshToken(presentedRefresh, clientID string) (*AccessTokenIssued, error) {
	hash := HashToken(presentedRefresh)
	now := time.Now().UTC()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var (
		oldID            string
		username, scope, audience string
		refreshExp       sql.NullInt64
		revokedAt        sql.NullInt64
		dbClientID       string
	)
	err = tx.QueryRow(
		`SELECT id, username, client_id, scope, audience, refresh_expires_at, revoked_at
		 FROM oauth_tokens WHERE refresh_token_hash = ?`, hash,
	).Scan(&oldID, &username, &dbClientID, &scope, &audience, &refreshExp, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOAuthTokenInvalid
	}
	if err != nil {
		return nil, err
	}
	if revokedAt.Valid || dbClientID != clientID {
		return nil, ErrOAuthTokenInvalid
	}
	if refreshExp.Valid && refreshExp.Int64 < now.Unix() {
		return nil, ErrOAuthTokenInvalid
	}

	if _, err := tx.Exec(
		`UPDATE oauth_tokens SET revoked_at = ?, refresh_token_hash = NULL WHERE id = ?`,
		now.Unix(), oldID,
	); err != nil {
		return nil, err
	}

	access, err := generateOAuthSecret(OAuthAccessPrefix)
	if err != nil {
		return nil, err
	}
	refresh, err := generateOAuthSecret(OAuthRefreshPrefix)
	if err != nil {
		return nil, err
	}
	newID := uuid.NewString()
	accessExp := now.Add(OAuthAccessTTL).Unix()
	refreshExpNew := now.Add(OAuthRefreshTTL).Unix()

	if _, err := tx.Exec(
		`INSERT INTO oauth_tokens
		 (id, access_token_hash, refresh_token_hash, client_id, username,
		  scope, audience, access_expires_at, refresh_expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, HashToken(access), HashToken(refresh), clientID, username,
		scope, audience, accessExp, refreshExpNew, now.Unix(),
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &AccessTokenIssued{
		ID:               newID,
		AccessToken:      access,
		RefreshToken:     refresh,
		AccessExpiresIn:  int(OAuthAccessTTL.Seconds()),
		RefreshExpiresIn: int(OAuthRefreshTTL.Seconds()),
		Scope:            scope,
		Audience:         audience,
	}, nil
}

// TouchOAuthLastUsed bumps last_used_at. Coalesced by the same
// usageUpdater that handles PATs.
func (s *Store) TouchOAuthLastUsed(id string) error {
	_, err := s.db.Exec(
		`UPDATE oauth_tokens SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Unix(), id,
	)
	return err
}

// ------------------------------------------------------------------
// helpers
// ------------------------------------------------------------------

// generateOAuthID returns a public identifier of the form
// `<prefix><base64url(N bytes)>`. Used for client_id; not secret.
func generateOAuthID(prefix string, nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// generateOAuthSecret returns 32 bytes of entropy with the given prefix.
// Used for client secrets, codes, access + refresh tokens — anything
// that lives in oauth_*_hash columns.
func generateOAuthSecret(prefix string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// validateRedirectURI enforces the OAuth 2.1 / MCP rule: redirect URIs
// MUST be HTTPS, with the loopback exception. Fragments are forbidden.
func validateRedirectURI(raw string) error {
	if raw == "" {
		return errors.New("empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Host == "" {
		return errors.New("must include host")
	}
	if u.Fragment != "" {
		return errors.New("must not contain fragment")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return errors.New("http only allowed for loopback")
	}
	return errors.New("must be https:// or http://localhost")
}

// VerifyPKCEChallenge returns true iff base64url(sha256(verifier)) == challenge.
// Used at /oauth/token to bind the code redemption to the original
// authorize request. Constant-time comparison.
func VerifyPKCEChallenge(verifier, challenge, method string) bool {
	if method != "S256" || verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	derived := base64.RawURLEncoding.EncodeToString(sum[:])
	return TokensEqual(derived, challenge)
}
