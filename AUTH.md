# AgentBoard authentication

## Goals

1. **Two credentials by audience.** Bearer tokens (`ab_*`, `oat_*`) for
   non-human callers (CLI, MCP clients, agents); a session cookie for
   humans in a browser. Same identity behind both — the choice is purely
   ergonomic: cookies get auto-attached + need CSRF, tokens are explicit +
   need no CSRF.
2. **Username is the identity.** Users are keyed by their username (not a
   UUID). `@alice` IS the user `alice` — no indirection.
3. **Usernames are immutable and forever-reserved.** Normal edits can't
   change a username; deactivation keeps the row and blocks re-use. This is
   what makes `@alice` in a 3-month-old page still mean the same alice.
4. **A user has many tokens.** Laptop, CI, Claude desktop — each labeled
   and individually rotatable. Rotating a token never changes attribution
   because writes record the username, not the token.
5. **Hard door on who can manage users.** Only admin-kind tokens (or an
   admin-kind session) can hit `/api/admin/*`. A member or bot never
   grants user-management.
6. **Emailless, filesystem-recoverable.** No SMTP, no recovery by email.
   Lockout recovery is `agentboard admin rotate` (token) or
   `agentboard admin set-password` / `revoke-sessions` (browser) on the
   host, or wiping the DB so boot re-mints a first-admin invitation URL.
7. **Per-user access scoping.** A user has an `access_mode` + `rules[]` that
   apply to every credential they own (token or session alike).

## Data model

Three tables: users, user_tokens, user_sessions.

```
users
  username             TEXT PRIMARY KEY COLLATE NOCASE  -- the identity
  display_name         TEXT                             -- free-form, mutable
  kind                 TEXT NOT NULL                    -- 'admin' | 'member' | 'bot'
  avatar_color         TEXT                             -- deterministic HSL from username, stored
  access_mode          TEXT NOT NULL                    -- 'allow_all' | 'restrict_to_list'
  rules_json           TEXT NOT NULL DEFAULT '[]'
  created_at           INTEGER NOT NULL
  created_by           TEXT                             -- another username
  deactivated_at       INTEGER                          -- soft delete; username stays reserved
  password_hash        TEXT                             -- argon2id; nullable (opt-in)
  password_updated_at  INTEGER                          -- unix; nullable when no password

user_tokens
  id              TEXT PRIMARY KEY                 -- uuid; tokens rotate so need their own id
  username        TEXT NOT NULL REFERENCES users(username)
  token_hash      TEXT UNIQUE NOT NULL             -- sha256(token)
  label           TEXT                             -- "laptop", "ci", "claude-desktop"
  created_at      INTEGER NOT NULL
  last_used_at    INTEGER
  revoked_at      INTEGER                          -- soft delete per token

user_sessions
  id              TEXT PRIMARY KEY                 -- uuid; one row per browser sign-in
  session_hash    TEXT UNIQUE NOT NULL             -- sha256(plaintext session value)
  username        TEXT NOT NULL REFERENCES users(username)
  created_at      INTEGER NOT NULL
  last_used_at    INTEGER
  expires_at      INTEGER NOT NULL                 -- absolute; default 30d after create
  revoked_at      INTEGER                          -- soft delete per session
  user_agent      TEXT                             -- captured at create, informational
  ip              TEXT                             -- captured at create, informational
```

Username rules: `^[a-z][a-z0-9_-]{0,31}$`. Lowercase letters, digits,
`_`, `-`. Starts with a letter, max 32 chars. Case-insensitive unique via
`COLLATE NOCASE` on the PK.

Tokens are `ab_<43 base64 chars>` — 32 bytes of entropy plus the `ab_`
prefix so secret scanners (GitHub push protection, gitleaks, truffleHog)
can spot them. The plaintext is returned exactly once at create/rotate;
thereafter only the sha256 hash is stored.

## Invariants (worth reading twice)

- **Usernames never change** via normal operations. The PATCH endpoint on
  `/api/admin/users/:username` doesn't accept `username`. The web UI shows
  username as read-only.
- **Usernames are reserved forever** — the `users` row stays on
  deactivation, so `INSERT INTO users (username, …)` for a deactivated name
  fails on the PK uniqueness. No re-use, ever, without a rename.
- **The only way to rename** is `agentboard admin rename-user <old> <new>`
  on the host. It updates `users.username` and `user_tokens.username` in a
  transaction. It does NOT rewrite free-text references (MDX page bodies,
  data-value strings, assignees arrays). The CLI warns; the operator
  decides whether to grep + rewrite.
- **All cross-references store the username as a string.**
  `data.updated_by = "alice"`, page bodies contain `@alice`, cards carry
  `assignees: ["alice", "bob"]`. No FK on the data plane — agents write
  plain strings — but attribution always resolves as long as the user row
  exists.

## HTTP auth

Non-open requests present **either** a bearer token or a session
cookie. Tried in priority order:

1. `Authorization: Bearer <token>` — agents, curl, MCP clients,
   anything not running in a browser. Both PATs (`ab_*`) and OAuth
   access tokens (`oat_*`) ride this path; the audience rule below
   distinguishes them.
2. HTTP Basic with `password=<token>` — browser prompt fallback for
   directly-typed URLs.
3. `?token=<token>` — EventSource bootstrap, one-click share links.
4. `agentboard_session` cookie — the SPA + the OAuth consent page.
   Triggered when no Authorization header is present and the cookie
   resolves to a valid `user_sessions` row.

`GET /api/health` and `/api/setup/status` stay open; the public
auth surface (`/api/auth/login`, `/api/auth/logout`, `/api/auth/me`)
is also open so the login round-trip can complete before a session
exists. Everything else resolves to `(User, UserToken)` or
`(User, Session)`. Missing/unknown/revoked/deactivated → 401.

Cookie-authenticated **state-changing** requests additionally pass
through `CSRFMiddleware`: an `X-CSRF-Token` header MUST equal the
`agentboard_csrf` cookie (constant-time compare). Missing or
mismatched → 403 with `code=CSRF_REQUIRED` / `CSRF_MISMATCH`. Bearer
requests skip — they're CSRF-immune by design.

Every 401 carries an MCP-spec-compliant Bearer challenge so OAuth-aware
clients (Claude.ai Custom Connectors, anything following the MCP
authorization spec) can discover the authorization server:

```
WWW-Authenticate: Bearer realm="AgentBoard",
                  resource_metadata="https://<host>/.well-known/oauth-protected-resource"
```

Browser top-level navigations also receive `WWW-Authenticate: Basic` so
the native auth prompt fires — paste the token as the password.

The admin realm has one additional middleware:

```
TokenMiddleware → AuthorizeMiddleware → AdminRequired → /api/admin/*
```

`AdminRequired` checks that the resolved user's kind is `admin` and rejects
everything else with 403.

## OAuth-issued tokens (Claude.ai Custom Connectors)

A second token shape exists for browser-driven MCP clients that can't
have a PAT pasted into them — Claude.ai Custom Connectors are the
motivating case. AgentBoard hosts an in-process OAuth 2.1 authorization
server alongside the MCP resource server, conformant to the MCP
authorization spec (RFC 9728 + RFC 8414 + RFC 7591 + OAuth 2.1 with
PKCE + RFC 8707 audience binding).

**The HTTP auth shape is unchanged**: presented credentials still arrive
as `Authorization: Bearer <token>`. What changes is the second token
*kind* the resolver recognizes, and the validation rules that apply to
those tokens.

### Token kinds, side by side

| Property | PAT (`ab_*`) | OAuth access (`oat_*`) |
|---|---|---|
| Minted by | `/api/admin/users/:u/tokens` or `agentboard admin rotate` | `/oauth/token` after authorize → consent |
| Lifetime | until revoked | 1 hour, refreshable for 30 days |
| Audience | none — accepted on every gated route | `<base>/mcp` only — rejected elsewhere with 401 |
| Stored in | `user_tokens` | `oauth_tokens` |
| Bound to | a user | a `(user, registered_client)` pair |
| Revocation | per token | per token, or implicit when the issuing user is deactivated |

OAuth tokens are deliberately scoped: the spec mandates audience
validation, and AgentBoard enforces it strictly. An `oat_*` token
presented at `/api/me` returns 401 — that's correct, not a bug.
Connectors don't need any other route.

### Endpoints

```
GET  /.well-known/oauth-protected-resource     # RFC 9728 metadata
GET  /.well-known/oauth-authorization-server   # RFC 8414 metadata
POST /oauth/register                           # RFC 7591 dynamic client registration
GET  /oauth/authorize                          # consent page (HTML)
POST /oauth/authorize/decide                   # form target — issues authorization code
POST /oauth/token                              # code → access token, or refresh rotation
```

All six are anonymous-readable / writable: the whole point is to
acquire a credential, so requiring one would deadlock.

### Flow

1. User pastes `https://<host>/mcp` into Claude.ai → Settings → Connectors → Add Custom.
2. Claude.ai fetches `/.well-known/oauth-protected-resource`, learns
   the authorization server URL, fetches its metadata.
3. Claude.ai POSTs to `/oauth/register` with redirect URIs and grant
   types → receives `client_id` (DCR — no manual setup).
4. Claude.ai opens the user's browser to `/oauth/authorize?...&code_challenge=...`.
5. AgentBoard renders an HTML consent page. The user pastes their
   AgentBoard PAT to authenticate the *consent decision* (the PAT is
   never shared with the client; it just proves the user owns this
   instance) and clicks Allow.
6. Browser is redirected to Claude.ai's callback with `code=oac_...`.
7. Claude.ai POSTs to `/oauth/token` with `code` + `code_verifier` →
   receives `access_token=oat_...` + `refresh_token=ort_...`,
   audience-bound to `<base>/mcp`.
8. Claude.ai uses `Authorization: Bearer oat_...` on every MCP call.
   Refresh rotates per OAuth 2.1 §4.3.1 — single-use, replays rejected.

### Spec conformance, item by item

- **OAuth 2.1 with PKCE S256** — required for all clients (`code_challenge_method=S256` enforced at `/oauth/authorize`).
- **RFC 7591 DCR** — `/oauth/register` accepts JSON metadata, returns `client_id` (and `client_secret` only for confidential clients).
- **RFC 9728 Protected Resource Metadata** — served at `/.well-known/oauth-protected-resource`, referenced from every 401's `WWW-Authenticate: Bearer ... resource_metadata="..."`.
- **RFC 8414 Authorization Server Metadata** — served at `/.well-known/oauth-authorization-server`, listing `code_challenge_methods_supported: ["S256"]`.
- **RFC 8707 Resource Indicators** — `resource` parameter accepted at `/authorize` and `/token`; defaults to canonical MCP URL when omitted; mismatches return `invalid_target`.
- **Audience binding** — every `oat_*` token carries an `audience` column. Middleware validates it equals `CanonicalMCPResourceURL(r)` on every MCP request and rejects mismatches.
- **Refresh token rotation** — `/oauth/token` with `grant_type=refresh_token` revokes the presented refresh and mints a new pair atomically. Replays of the old refresh return `invalid_grant`.

### Bootstrap quirks

The OAuth flow has one prerequisite: the user must already have a way
to authenticate the consent step — either an `ab_*` PAT to paste, or
a username + password (the new browser-friendly path). Fresh instances
bootstrap admin via the first-admin invitation URL; the redeem flow
also accepts an optional password and emits a session cookie, so a
freshly-claimed admin can drive the connector flow without ever
copying a token. There's no chicken-and-egg.

## Browser sessions (passwords + cookies)

Tokens are a great fit for agents and CLI but a clunky one for humans
in a browser. AgentBoard's second credential mechanism — added on top
of the token model, not in place of it — fixes that:

- **Sign in**: `POST /api/auth/login` with `{username, password}`.
  Server verifies via `argon2id`, mints a row in `user_sessions`,
  returns the user record, and sets two cookies:
  - `agentboard_session` — `HttpOnly`, `SameSite=Lax`, `Secure` when
    the request was TLS, `Path=/`, ~30-day TTL. The plaintext is the
    high-entropy random value from `auth.GenerateSessionPlaintext`
    (`abs_<43 base64 chars>`). The DB stores `sha256(plaintext)`.
  - `agentboard_csrf` — same shape, NOT HttpOnly. The SPA reads it
    via `document.cookie` and copies into `X-CSRF-Token` on every
    state-changing request.
- **Verify**: middleware extracts the session cookie when there's no
  `Authorization` header, runs `ResolveSession`, attaches the User
  to context exactly as it would for a token.
- **CSRF**: `CSRFMiddleware` runs after auth and enforces double-
  submit on state-changing requests **only** when authentication came
  from a session cookie. Bearer-authenticated requests skip — Bearer
  is not auto-attached by the browser, so cross-origin attacks can't
  smuggle one along.
- **Sign out**: `POST /api/auth/logout` revokes the row and clears
  both cookies. Idempotent — a stale cookie still triggers cookie-
  clearing on the response.

Where humans currently land: `/login` (username + password form) and
`/oauth/authorize` (the consent page now branches on session — when
present, "Logged in as @user · Allow / Deny"; when absent, password
+ token paste, in that order). Both unify the same lookup table.

Setting + clearing passwords:

- **Self-or-admin**: `POST /api/users/{u}/password` with
  `{current_password?, new_password}`. A self-call without
  `current_password` is rejected (proof-of-possession); admins
  setting another user's password may omit it.
- **CLI**: `agentboard admin set-password <u>` (interactive prompt,
  or `--from-stdin` for scripts). Lockout-recovery hammer: file-
  system access, no token required.
- **Revocation**: per-row at `DELETE /api/users/{u}/sessions/{id}`,
  bulk at `POST /api/users/{u}/sessions/revoke-all`, or CLI
  `agentboard admin revoke-sessions <u>`. Bearer tokens are NOT
  touched.

Token vs session, side by side:

| Property | PAT (`ab_*`) | Browser session (`abs_*`) |
|---|---|---|
| Minted by | `/api/users/{u}/tokens` or `agentboard admin rotate` | `/api/auth/login` after username + password |
| Carrier | `Authorization: Bearer …` (header) | `agentboard_session` cookie |
| Lifetime | until revoked | 30 days, refreshable by re-login |
| CSRF protection | not required (header is opt-in per request) | required (`X-CSRF-Token` matched against `agentboard_csrf` cookie) |
| Logout shape | revoke the token row | clear cookie + revoke session row |
| Stored as | sha256 hash in `user_tokens` | sha256 hash in `user_sessions` |

Passkeys / WebAuthn are still deferred. The session table shape was
designed to accept a passkey factor as a second auth path without
schema churn — adding `factor TEXT NOT NULL DEFAULT 'password'` (or
similar) is the only change foreseen.

## Rules engine

Each user has:

- `access_mode`: `allow_all` (default) or `restrict_to_list`
- `rules`: ordered `[{action, pattern, methods}]`

Evaluation on each agent-kind request:

1. Walk rules top-to-bottom. First rule whose `pattern` matches the path
   AND `methods` includes the request method decides.
2. If no rule matched, fall back to `access_mode`:
   - `allow_all` → allow.
   - `restrict_to_list` → deny (403).

**Admin users are exempt.** Full access, rules not consulted. Avoids the
"admin wrote a bad rule and locked themselves out" footgun.

### Pattern syntax

Glob only. Three tokens:

- Literal chars and `/` match themselves.
- `*` matches any run of characters except `/`.
- `**` matches any run of characters including `/`.

Ergonomic shortcut: `foo/**` also matches `foo` exactly, so
`/api/data/**` covers both `/api/data` and `/api/data/dev.metrics`.

### Common recipes

**Read-only viewer**:
```json
{
  "access_mode": "restrict_to_list",
  "rules": [
    {"action":"allow","pattern":"/api/data/**","methods":["GET"]},
    {"action":"allow","pattern":"/api/content/**","methods":["GET"]},
    {"action":"allow","pattern":"/api/files/**","methods":["GET"]},
    {"action":"allow","pattern":"/api/events","methods":["GET"]}
  ]
}
```

**Ops agent, secrets quarantined**:
```json
{
  "access_mode": "allow_all",
  "rules": [
    {"action":"deny","pattern":"/api/data/secrets.**","methods":["*"]}
  ]
}
```

## Bootstrap + recovery

- **First run (Auth v1 flow)**: `serve` notices no users exist, mints a
  role=admin invitation, prints its `/invite/<id>` URL to stdout AND
  writes it to `<project>/.agentboard/first-admin-invite.url`. Operator
  opens the URL in a browser, picks a username, and receives the first
  admin token. Idempotent across restarts; if the existing bootstrap
  invite expires, a fresh one is minted automatically.
- **Legacy `AGENTBOARD_AUTH_TOKEN`**: if set on first boot, a
  `@legacy-agent` user is created (kind=member) with that token as their
  "legacy" token. Existing curl clients keep working. In that case the
  bootstrap invitation is NOT minted because an identity already exists.
- **Lockout recovery**: run `agentboard admin list-invitations` on the
  host to re-reveal any active invite URL, or `agentboard admin rotate
  <user> <label>` to mint a fresh token value for an existing slot.
  If every admin is lost, delete the DB so the next `serve` re-emits a
  fresh first-admin invitation URL.
- **Rotation**: `agentboard admin rotate <username> [label]` on the host,
  or the "Rotate" button per-token in the UI. Old token stops working
  immediately.
- **Rename (typo recovery)**: `agentboard admin rename-user <old> <new>`
  on the host. Updates `users` + `user_tokens` transactionally. Confirm
  prompt unless `--yes`. See the invariants section for what this does
  NOT rewrite.

## HTTP endpoints

### Agent realm (token-gated)

`/api/data/*`, `/api/content/*`, `/api/files/*`, `/api/skills/*`,
`/api/errors`, `/api/grab`, `/api/events`, `/mcp`. Rules narrow access
per-user.

PATs (`ab_*`) accepted everywhere in this list; OAuth access tokens
(`oat_*`) accepted on `/mcp` only — see "OAuth-issued tokens" above
for the audience-scoping rule.

### Admin realm (admin-token-gated)

```
GET    /api/admin/me
GET    /api/admin/users
POST   /api/admin/users
PATCH  /api/admin/users/:username                      -- display_name, access_mode, rules
POST   /api/admin/users/:username/deactivate
POST   /api/admin/users/:username/tokens               -- body: {label?}
POST   /api/admin/users/:username/tokens/:tokenId/rotate
POST   /api/admin/users/:username/tokens/:tokenId/revoke
```

All use `:username`, not an opaque id. URLs are grep-friendly:
`/api/admin/users/alice/tokens`.

### User directory (any authenticated token)

```
GET  /api/users                 -- list of public user records
POST /api/users/resolve         -- body: {usernames: [...]} → matching records
```

Public records are `{username, display_name, kind, avatar_color,
deactivated}`. Never include tokens, rules, or access_mode. Used by
@mention autocomplete and assignee validators.

## Frontend

One route: `/admin`, rendered inside the normal Layout (sidebar included).
The SPA reads an admin token from `localStorage["agentboard:admin-token"]`.
Missing or rejected → single-field token prompt; submitting stores the
token and continues.

- User cards expand to show per-user token lists (label, last-used, rotate,
  revoke) and the "Add token" button.
- Rules editor is a JSON textarea with full-access / viewer / custom
  templates. Good enough for now.
- Display name, access mode, and rules are editable; username is
  read-only with a tooltip pointing at the CLI rename command.

## CLI

```
agentboard admin list                          # users + token counts
agentboard admin list-invitations              # active invite URLs (incl. the first-admin one)
agentboard admin invite [--role …]             # mint a new invite URL without a token
agentboard admin rotate <username> [label]     # rotate a token
agentboard admin set-password <username>       # set/replace browser password
agentboard admin revoke-sessions <username>    # nuke every active browser session for a user
agentboard admin rename-user <old> <new> [--yes]  # escape hatch
```

Gated by filesystem access — anyone who can read the SQLite file already
has its contents, so network-less CLI recovery is the right trust shape.

## MCP invariant

MCP is the agent realm. No MCP tool may expose user, token, rotation,
revocation, or any admin capability. A CI test in
`internal/mcp/privilege_test.go` iterates the tool catalog and fails the
build if a new tool's name or description contains forbidden substrings.

## Mentions + assignments (planned, not yet shipped)

The data model is already set up for this:

- `@alice` in free text → parsed at render time via a `<RichText>`
  component that calls `POST /api/users/resolve` and replaces matches
  with a `<Mention>` badge. Plain text on disk; no pre-resolved shapes.
- Cards/tasks accept a top-level `assignees: ["alice", "bob"]` field.
  Components (Kanban, Table, List) look for it and render avatars.
- Remark plugin for MDX pages does the same @username → `<Mention>`
  transformation at compile time.

Nothing about the auth schema needs to change to support these.

## Threat model

| Threat | Outcome |
|---|---|
| Leaked member token | What their rules allow. Can't reach admin paths. Admin revokes → done. |
| Leaked viewer token | Read-only within allowlist. |
| Leaked admin token | Full management access until revoked. Rotate regularly; keep admin tokens out of CI logs. |
| Leaked bot token | Shared puppet; any admin rotates. |
| Leaked session cookie | Same blast radius as the user's role; `agentboard admin revoke-sessions <u>` is the recovery hammer. HttpOnly + SameSite=Lax mitigates XSS leakage and cross-origin auto-submit. |
| Leaked invitation URL | One-time use. Once redeemed it's dead; admins can revoke unredeemed invites from `/admin`. |
| SSH / filesystem access | Total. Intended recovery layer — `admin rotate` + `admin set-password` + DB wipe-for-first-admin-reinvite all route through it. |
| Malicious MCP tool added | Blocked by the privilege test in CI. |
| CSRF on cookie auth | Double-submit `X-CSRF-Token` enforced on every state-changing route reachable via cookie; bearer skips. |
| Token brute force | `sha256(32 bytes random)`. 2^256 attempts. |
| Password brute force | `argon2id(time=1, memory=64MiB, threads=4)` per attempt. Slow enough that online brute-force is impractical; CLI lockout-recovery is the intended out-of-band path. |
| Username confusion attacks | `COLLATE NOCASE` on the PK and Go-side lowercase-trim on every insert path. |

## Passkey / WebAuthn — deferred

Adds later as a way to get the admin token into the SPA (e.g. "unlock from
keychain via passkey challenge"). Storage and HTTP shape stay token-based;
passkeys become a UX layer for retrieving the bearer, not a replacement.
No schema changes required.

## File layout

```
internal/auth/
  schema.go         — users + user_tokens + user_sessions schema, v1→v2 migration
  username.go       — regex validator + avatar color deriver
  tokens.go         — GenerateToken, HashToken, TokensEqual
  passwords.go      — argon2id HashPassword + VerifyPassword
  sessions.go       — SetPassword, VerifyLogin, CreateSession, ResolveSession, Revoke*
  store.go          — Users + Tokens CRUD, RenameUser, ResolveToken, ResolveUsernames
  rules.go          — glob matcher + Authorize
  rules_test.go
  store_test.go
  sessions_test.go
  passwords_test.go
  middleware.go     — TokenMiddleware (PAT + OAuth + session cookie), CSRFMiddleware, AuthorizeMiddleware, AdminRequired, ScopeSelfOrAdmin
  oauth.go          — RFC 9728/8414 discovery handlers + WWW-Authenticate helper
  oauth_schema.go   — oauth_clients, oauth_codes, oauth_tokens
  oauth_store.go    — DCR, code lifecycle, access-token + refresh rotation, PKCE verifier
  migrate.go        — BootstrapFirstAdmin

internal/invitations/
  invitations.go  — Create/Get/List/Revoke/Redeem + BootstrapActive

internal/locks/
  locks.go        — Lock/Unlock/IsLocked/Rename (page-level admin freeze)

internal/server/
  handlers_admin.go        — /api/admin/users/* routes
  handlers_auth.go         — /api/auth/{login,logout,me} + /api/users/{u}/{password,sessions}
  handlers_tokens.go       — /api/users/{u}/tokens/* (self-or-admin)
  handlers_auth_test.go    — login / logout / me / CSRF coverage
  handlers_invitations.go  — /api/admin/invitations + public /api/invitations/{id}[/redeem] (accepts an optional password)
  handlers_locks.go        — /api/locks CRUD + enforcePageLock helper
  handlers_users.go        — /api/users + /api/users/resolve (authed-read directory)
  handlers_oauth.go        — /oauth/{register,authorize,authorize/decide,token} (consent accepts session cookie OR username+password OR pasted token)
  handlers_oauth_test.go   — discovery + full PKCE flow + audience-scoping + session-consent coverage

internal/cli/
  admin.go        — list / list-invitations / rotate / set-password / revoke-sessions / rename-user / invite

internal/mcp/
  privilege_test.go

frontend/src/
  lib/session.ts  — apiFetch (cookie + CSRF), signInWithPassword, signOut, fetchSessionUser
  lib/auth.ts     — token + invitation + session + password REST helpers
  routes/Login.tsx       — username + password form
  routes/InviteRedeem.tsx — invitation redeem also accepts a password + sets a cookie session
  routes/Tokens.tsx      — per-user tokens + active sessions + change-password
  routes/Admin.tsx       — admin user list with set-password / revoke-sessions controls
```
