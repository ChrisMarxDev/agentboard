# AgentBoard authentication

## Goals

1. **One credential mechanism everywhere.** One token space, one HTTP auth
   shape (`Authorization: Bearer`). No passwords, no sessions, no cookies,
   no CSRF.
2. **Username is the identity.** Users are keyed by their username (not a
   UUID). `@alice` IS the user `alice` — no indirection.
3. **Usernames are immutable and forever-reserved.** Normal edits can't
   change a username; deactivation keeps the row and blocks re-use. This is
   what makes `@alice` in a 3-month-old page still mean the same alice.
4. **A user has many tokens.** Laptop, CI, Claude desktop — each labeled
   and individually rotatable. Rotating a token never changes attribution
   because writes record the username, not the token.
5. **Hard door on who can manage users.** Only admin-kind tokens can hit
   `/api/admin/*`. A member or bot token never grants user-management.
6. **Emailless, filesystem-recoverable.** No SMTP, no recovery by email.
   Lockout recovery is `agentboard admin rotate` on the host, or wiping
   the DB so boot re-mints a first-admin invitation URL.
7. **Per-user access scoping.** A user has an `access_mode` + `rules[]` that
   apply to every token they own.

## Data model

Two tables.

```
users
  username        TEXT PRIMARY KEY COLLATE NOCASE  -- the identity
  display_name    TEXT                             -- free-form, mutable
  kind            TEXT NOT NULL                    -- 'admin' | 'agent'
  avatar_color    TEXT                             -- deterministic HSL from username, stored
  access_mode     TEXT NOT NULL                    -- 'allow_all' | 'restrict_to_list'
  rules_json      TEXT NOT NULL DEFAULT '[]'
  created_at      INTEGER NOT NULL
  created_by      TEXT                             -- another username
  deactivated_at  INTEGER                          -- soft delete; username stays reserved

user_tokens
  id              TEXT PRIMARY KEY                 -- uuid; tokens rotate so need their own id
  username        TEXT NOT NULL REFERENCES users(username)
  token_hash      TEXT UNIQUE NOT NULL             -- sha256(token)
  label           TEXT                             -- "laptop", "ci", "claude-desktop"
  created_at      INTEGER NOT NULL
  last_used_at    INTEGER
  revoked_at      INTEGER                          -- soft delete per token
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

Non-open requests present a token via:

- `Authorization: Bearer <token>` — agents, curl, MCP clients, the admin UI
- HTTP Basic with `password=<token>` — browser prompt fallback
- `?token=<token>` — EventSource bootstrap, one-click share links

`GET /api/health` stays open. Everything else resolves the token →
`(User, UserToken)`. Missing/unknown/revoked/deactivated → 401.

The admin realm has one additional middleware:

```
TokenMiddleware → AuthorizeMiddleware → AdminRequired → /api/admin/*
```

`AdminRequired` checks that the resolved user's kind is `admin` and rejects
everything else with 403.

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
agentboard admin list                    # users + token counts
agentboard admin list-invitations        # active invite URLs (incl. the first-admin one)
agentboard admin rotate <username> [label]  # rotate a token
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
| Leaked invitation URL | One-time use. Once redeemed it's dead; admins can revoke unredeemed invites from `/admin`. |
| SSH / filesystem access | Total. Intended recovery layer — `admin rotate` + DB wipe-for-first-admin-reinvite both route through it. |
| Malicious MCP tool added | Blocked by the privilege test in CI. |
| CSRF | N/A — no cookies, no auto-attached credentials. |
| Token brute force | `sha256(32 bytes random)`. 2^256 attempts. |
| Username confusion attacks | `COLLATE NOCASE` on the PK and Go-side lowercase-trim on every insert path. |

## Passkey / WebAuthn — deferred

Adds later as a way to get the admin token into the SPA (e.g. "unlock from
keychain via passkey challenge"). Storage and HTTP shape stay token-based;
passkeys become a UX layer for retrieving the bearer, not a replacement.
No schema changes required.

## File layout

```
internal/auth/
  schema.go       — users + user_tokens schema
  username.go     — regex validator + avatar color deriver
  tokens.go       — GenerateToken, HashToken, TokensEqual
  store.go        — Users + Tokens CRUD, RenameUser, ResolveToken, ResolveUsernames
  rules.go        — glob matcher + Authorize
  rules_test.go
  store_test.go
  middleware.go   — TokenMiddleware, AuthorizeMiddleware, AdminRequired, ScopeSelfOrAdmin
  migrate.go      — BootstrapFirstAdmin

internal/invitations/
  invitations.go  — Create/Get/List/Revoke/Redeem + BootstrapActive

internal/locks/
  locks.go        — Lock/Unlock/IsLocked/Rename (page-level admin freeze)

internal/server/
  handlers_admin.go        — /api/admin/users/* routes
  handlers_tokens.go       — /api/users/{u}/tokens/* (self-or-admin)
  handlers_invitations.go  — /api/admin/invitations + public /api/invitations/{id}[/redeem]
  handlers_locks.go        — /api/locks CRUD + enforcePageLock helper
  handlers_users.go        — /api/users + /api/users/resolve (authed-read directory)

internal/cli/
  admin.go        — list / list-invitations / rotate / rename-user

internal/mcp/
  privilege_test.go

frontend/src/
  lib/auth.ts
  routes/Admin.tsx
```
