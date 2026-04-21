# AgentBoard authentication

> **Status:** active design, replacing the legacy single-token gate from v0.2.
> The legacy `AGENTBOARD_AUTH_TOKEN` env var still works during migration; see
> the [migration](#migration-from-the-legacy-single-token) section at the end.

## Goals

1. **Hard separation between "human admins" and "agent tokens."** Stealing an
   agent token must not let an attacker create more tokens, escalate, or lock
   the admin out.
2. **Emailless.** No SMTP dependency anywhere. No "forgot password via email."
3. **Humans distribute tokens, not agents.** The flow assumes a human admin
   creates tokens in a browser and hands them to agents / viewers / CI bots.
4. **Revocable and rotatable.** Every token has a name, a last-used timestamp,
   and can be revoked or rotated individually. No "one token for everything."
5. **Per-identity access scoping.** Tokens can be restricted to specific paths
   and methods via an allowlist or blocklist of glob rules.
6. **Passkey-extensible.** Passwords ship first, but the architecture must not
   need changes to add WebAuthn/passkeys later.

## The two auth realms

Agent tokens and admin sessions are two **disjoint** credential classes. No
request can cross from one to the other without a fresh authentication event.

| Realm | Credential | Presented as | Gates |
|---|---|---|---|
| **Agent** | Token (`ab_...`) | `Authorization: Bearer`, HTTP Basic password, or `?token=` query | `/api/data/*`, `/api/content/*`, `/api/files/*`, `/api/skills/*`, `/mcp`, `/api/grab/*`, SSE |
| **Admin** | Session cookie | `Cookie: ab_session=...` (HTTP-only, Secure, SameSite=Strict) | `/api/admin/*` only |

The public endpoint `GET /api/health` remains open.

**Agent tokens cannot hit `/api/admin/*`.** The admin session cookie cannot
be minted without presenting the admin password (or, later, a passkey). An
agent impersonating the admin needs the password itself — not the token.

**Admin sessions cannot hit MCP or the data API as anything other than the
admin's own identity.** Admins who want to write data as an agent use an
agent token like everyone else. Keeping the two realms disjoint means the
middleware is trivial to reason about.

## Identity model

One table covers both admins and agents:

```
identities
  id              TEXT PRIMARY KEY     -- uuid
  name            TEXT UNIQUE NOT NULL -- human label, also used as updated_by
  kind            TEXT NOT NULL        -- 'admin' | 'agent'
  token_hash      TEXT                 -- sha256(token), NULL for admin rows
  password_hash   TEXT                 -- argon2id, NULL for agent rows
  access_mode     TEXT NOT NULL        -- 'allow_all' | 'restrict_to_list'
  rules_json      TEXT NOT NULL DEFAULT '[]'
  created_at      INTEGER NOT NULL
  created_by      TEXT                 -- id of the identity that created this one
  last_used_at    INTEGER
  revoked_at      INTEGER              -- soft delete; keep for audit
```

Constraints:

- `kind = 'admin'` rows have a `password_hash` and no `token_hash`. They
  authenticate via the password → session flow.
- `kind = 'agent'` rows have a `token_hash` and no `password_hash`.
- Admin identities can still carry `access_mode` and `rules_json` — but admin
  routes don't consult them (admin = full trust). The fields exist so that if
  an admin ever wants to call data-plane APIs through their *own* agent token
  alongside, the schema is uniform.
- `name` is globally unique so `updated_by` on every write is unambiguous.

Tokens are prefixed `ab_` and consist of 32 bytes of URL-safe base64:
`ab_<43 chars>`. The prefix makes them log-greppable and scannable by
git-secrets / truffleHog / GitHub push protection.

## Session model

```
admin_sessions
  id              TEXT PRIMARY KEY     -- random opaque id, what goes in the cookie
  identity_id     TEXT NOT NULL REFERENCES identities(id)
  created_at      INTEGER NOT NULL
  last_seen_at    INTEGER NOT NULL
  expires_at      INTEGER NOT NULL
  user_agent      TEXT
  ip              TEXT
```

Rules:

- **Idle timeout**: 2h — any request bumps `last_seen_at`, inactivity past 2h
  invalidates the session.
- **Absolute timeout**: 7d — cannot be extended, forces re-login.
- **Cookie flags**: `HttpOnly; Secure; SameSite=Strict; Path=/api/admin`.
- **CSRF**: SameSite=Strict is the primary defense; admin endpoints that
  mutate state also require an `X-CSRF-Token` header that matches a session
  field, defense in depth. (Browsers with a valid cookie can fetch it from
  `GET /api/admin/me`.)
- Sessions are purged on password change (forced re-login everywhere) and on
  admin account revocation.

## Bootstrap

The install flow has to produce an admin on an empty database, without using
email and without shipping a default password.

```
[installer]                    [DB]                      [browser]
     │                           │                           │
     │ generate 32-char code     │                           │
     ├──────────────────────────▶│ bootstrap_codes            │
     │                           │   hash(code), expires_at  │
     │                           │                           │
     │ print code to operator    │                           │
     │                           │                           │
                                                             │
                          operator visits /setup in browser  │
                                                             │
                                  │    POST /api/admin/setup │
                                  │◀──────────────────────────
                                  │ { code, name, password } │
                                  │                           │
                                  │ verify code, create        │
                                  │ admin identity,            │
                                  │ consume code               │
                                  │                           │
                                  │ issue session cookie       │
                                  │──────────────────────────▶│
                                                             │
```

Bootstrap codes:

```
bootstrap_codes
  id              TEXT PRIMARY KEY
  code_hash       TEXT NOT NULL UNIQUE  -- sha256(code)
  created_at      INTEGER NOT NULL
  expires_at      INTEGER NOT NULL      -- 24h from creation
  used_at         INTEGER               -- set when consumed; not reusable
```

There can be multiple active codes at a time (re-issuing on the CLI doesn't
invalidate older codes; admins can invalidate explicitly). Consuming a code
marks it `used_at`; it cannot be used twice.

After the first admin exists, `/setup` still works: additional codes can be
issued by a logged-in admin to onboard another admin, or by the CLI. There
is no special "first-run" mode that latches off — just "does a valid code
exist that matches the submitted one."

## CLI admin commands

Gated by filesystem access to the SQLite DB (same trust layer as reading
`/etc/agentboard/env` today). Intended for recovery and for the `deploy-vps.sh`
installer.

```
agentboard admin bootstrap-code [--ttl 24h]
    # Generates a new code, prints it to stdout, stores hash in bootstrap_codes.
    # Used by the installer at first run, and by operators for lockout recovery.

agentboard admin reset
    # Generates a new bootstrap code AND marks all existing admin identities
    # as requiring re-setup (password_hash cleared). Kills all sessions.
    # Use when the only admin forgot their password. Destructive — confirms first.

agentboard admin list
    # Prints identities with name, kind, last_used_at, revoked_at. No tokens.
```

No CLI command prints existing tokens. Tokens are only visible once, at the
moment of creation / rotation, in the UI or in the token-create HTTP response.

## Rules engine (per-identity access scoping)

Each agent identity has:

- `access_mode`: `allow_all` (default) or `restrict_to_list`
- `rules`: ordered list of `{action, pattern, methods}`

Evaluation on each incoming request:

1. Token-auth middleware resolves the Bearer token → identity.
2. If `identity.revoked_at IS NOT NULL` → 401.
3. Walk `rules` top-to-bottom. First rule whose `pattern` glob-matches the
   request path **and** whose `methods` includes the request method wins.
4. If a rule matched: its `action` (`allow` / `deny`) decides.
5. If no rule matched: fall back to `access_mode`.
   - `allow_all`: request allowed.
   - `restrict_to_list`: request denied with 403.

### Pattern syntax

Globs, not regex. Supports `*` (matches any run of chars except `/`), `**`
(matches any run of chars including `/`), and exact segments.

Examples:

| Pattern | Matches |
|---|---|
| `/api/data/**` | any data operation |
| `/api/data/dev.*` | data under `dev.` but not nested dots |
| `/api/data/dev.**` | anything starting with `dev.` including nested dots |
| `/api/content/private/**` | pages under `private/` |
| `/mcp` | MCP JSON-RPC endpoint |

`methods` is a list — `["GET"]` for read-only tokens, `["GET","POST","PUT","PATCH","DELETE"]`
or just `["*"]` for full access.

### Common shapes

**Read-only viewer** (default mode `restrict_to_list`):
```json
[
  {"action":"allow","pattern":"/api/data/**","methods":["GET"]},
  {"action":"allow","pattern":"/api/content/**","methods":["GET"]},
  {"action":"allow","pattern":"/api/files/**","methods":["GET"]},
  {"action":"allow","pattern":"/api/subscribe","methods":["GET"]}
]
```

**Marketing-dashboard-only agent** (default mode `restrict_to_list`):
```json
[
  {"action":"allow","pattern":"/api/data/marketing.**","methods":["*"]},
  {"action":"allow","pattern":"/api/content/marketing/**","methods":["*"]}
]
```

**Ops agent with secret exclusion** (default mode `allow_all`):
```json
[
  {"action":"deny","pattern":"/api/data/secrets.**","methods":["*"]}
]
```

### Admin-plane is never rule-gated

Rules apply to agent tokens on data-plane endpoints. Admin sessions always
have full admin access; they have no rules list. This avoids the "admin
restricted themselves out of the admin UI" footgun.

## HTTP endpoints

### Agent-realm (token-gated) — no change in shape

Everything from v0.2 still works. The only behavioral change: the server
identifies the specific identity behind a token, so `updated_by` on writes
comes from `identities.name` instead of the free-text `X-Agent-Source` header.
The header is still honored for backwards compat but is advisory.

### Admin-realm (session-gated)

```
POST   /api/admin/setup
  body: { "code": "...", "name": "alice", "password": "..." }
  preconditions: submitted code matches an unexpired, unused bootstrap_code
  effect: creates an admin identity, consumes the code, returns session cookie
  response: 201 + Set-Cookie

POST   /api/admin/login
  body: { "name": "alice", "password": "..." }
  rate-limit: 5/min per IP, 10/min per name
  response: 200 + Set-Cookie on success, 401 on failure (constant-time compare)

POST   /api/admin/logout
  effect: deletes current session, clears cookie

GET    /api/admin/me
  response: { id, name, csrf_token }

POST   /api/admin/password
  body: { "current": "...", "new": "..." }
  effect: updates password_hash, invalidates all sessions for this admin
          except the current one

GET    /api/admin/identities
  response: list of identities with name, kind, access_mode, last_used_at,
            revoked_at. Never includes token_hash or password_hash.

POST   /api/admin/identities
  body: { "name": "...", "kind": "agent"|"admin", "access_mode": "...",
          "rules": [...], "password": "..." (admin only) }
  response: { id, name, token: "ab_..." } — token returned once; never again.

PATCH  /api/admin/identities/:id
  body: any of { "name", "access_mode", "rules" }
  response: updated identity

POST   /api/admin/identities/:id/rotate
  body: { "mode": "hard"|"graceful", "grace_period": "24h" (graceful only) }
  response: { new_token: "ab_..." }
  effect: hard → token_hash replaced immediately. graceful → both old and
          new tokens accepted until grace_period expires.

POST   /api/admin/identities/:id/revoke
  effect: sets revoked_at; the token stops working on the next request.

POST   /api/admin/bootstrap-codes
  response: { code: "...", expires_at: ... }
  effect: as the CLI command, but admin-authenticated.

GET    /api/admin/bootstrap-codes
  response: list of outstanding codes (hash prefix only, never the code itself)

DELETE /api/admin/bootstrap-codes/:id
  effect: invalidates an outstanding code
```

## Frontend surface

Three new routes in the SPA:

- `/setup` — visible only when a valid bootstrap code is required to enter.
  Form: bootstrap code, admin name, password (twice). On success, lands in
  `/admin`.
- `/login` — admin login. Form: name, password. Rate-limited server-side.
- `/admin` — identity management. Lists identities, lets admin create / edit /
  rotate / revoke. Shows the plaintext token exactly once (on create or rotate)
  in a copy-to-clipboard modal; after that it's unrecoverable from the UI.
- `/admin/settings` — change password, view active sessions, issue bootstrap
  codes.

The rest of the SPA (dashboards, pages, etc.) is unchanged. It's still gated
by agent-token auth — a browser visitor pastes an agent token, the SPA stores
it in localStorage, and subsequent requests carry `Authorization: Bearer`.
Admin-only navigation (link to `/admin`) appears only when a valid admin
session cookie is present (detected via `GET /api/admin/me`).

## MCP invariant

MCP tools are agent-token-gated. No MCP tool exposes any admin capability.

This must be enforced by a test that iterates the MCP tool catalog and
asserts each tool's handler only touches data/content/files/skills — not
identities, sessions, or bootstrap codes. Proposed test location:
`internal/mcp/privilege_test.go`. If someone adds `create_identity` as an
MCP tool in the future, that test fails loudly.

## Threat model

What this protects against:

| Threat | Outcome |
|---|---|
| Leaked agent token | Attacker can read/write what that identity's rules permit. Cannot create more tokens, cannot revoke the admin, cannot touch other identities' data if `restrict_to_list` was set. Admin revokes → done. |
| Leaked viewer token | Read-only. Limited to whatever paths were allowed. |
| Phished admin password | Attacker gets full admin. Same risk as any password-based system. Mitigation: passkey support (deferred). |
| CSRF against admin UI | Blocked by SameSite=Strict + `X-CSRF-Token` on mutating endpoints. |
| Brute-force password | Rate-limited (5/min per IP, 10/min per name), argon2id timing constant. |
| Replayed session cookie | Session tied to `last_seen_at` + absolute expiry. Sessions purged on password change. |
| SSH access to the host | Full compromise. This is the correct trust boundary — filesystem access to SQLite always wins. `admin reset` is the intended recovery path through this layer. |
| Malicious MCP tool add | Blocked by the privilege test — CI fails if a new tool can touch admin paths. |

What this does **not** protect against:

- An admin creating a badly-scoped token and leaking it — garbage-in, garbage-out. Mitigation is UX: the create-token UI has good defaults (e.g. viewer templates) and shows the effective rule evaluation.
- Supply-chain compromise of AgentBoard itself. Separate concern; see `seams_to_watch.md`.
- Network-level attacks on the TLS terminator. Outside the app.

## Passkey extension (deferred)

The architecture is designed so that adding WebAuthn is additive:

- New table `webauthn_credentials(identity_id, credential_id, public_key, sign_count, ...)`.
- New endpoints `POST /api/admin/login/webauthn/begin` and
  `.../finish` alongside the existing `POST /api/admin/login`.
- Both endpoints issue the same `admin_sessions` cookie. No admin route
  changes.
- Setup flow grows a "register passkey" step after password setup (or instead
  of it, if we eventually want passwordless admins).

Nothing in the data-plane middleware, rules engine, or agent-token paths
changes. This is why landing sessions as the unit of authn is load-bearing.

## Migration from the legacy single-token

On server startup, the migration checks:

1. Does the `identities` table have any `admin` row? → No migration needed.
2. Is `AGENTBOARD_AUTH_TOKEN` set in the env? → Yes:
   - Create one admin identity named `legacy-admin` with a **random password**.
     Print a warning at startup telling the operator to run
     `agentboard admin reset` to set a new password, or use the web UI to set
     one after visiting `/setup` with a freshly generated bootstrap code.
   - Create one agent identity named `legacy-agent` whose `token_hash` is
     `sha256(AGENTBOARD_AUTH_TOKEN)`, `access_mode=allow_all`, no rules. Every
     existing curl command / agent keeps working.
   - Leave the env var gate in place for 1 minor version, then remove.
3. Is `AGENTBOARD_AUTH_TOKEN` unset? → Server runs open (same as today on
   loopback). Print the same warning as before plus a nudge to run
   `agentboard admin bootstrap-code`.

After migration, the env var is ignored if any admin identity exists. The
DB is the source of truth.

## File layout (implementation reference)

```
internal/auth/
  identities.go       — CRUD for identities table, token hashing
  sessions.go         — admin_sessions CRUD, cookie issuance
  bootstrap.go        — bootstrap_codes, setup/reset flows
  rules.go            — glob engine, rule evaluation
  rules_test.go       — glob + evaluation tests
  middleware.go       — token-auth and session-auth middlewares
  migrate.go          — one-shot migration from AGENTBOARD_AUTH_TOKEN

internal/server/
  handlers_admin.go   — /api/admin/* endpoints
  middleware_auth.go  — replaced by internal/auth/middleware.go, shim kept

cmd/agentboard/
  admin_cmds.go       — cobra "admin bootstrap-code" / "admin reset" / "admin list"

frontend/src/
  routes/setup.tsx
  routes/login.tsx
  routes/admin/index.tsx
  routes/admin/identity.tsx
  routes/admin/settings.tsx
  lib/auth.ts         — fetch helpers, admin session detection
```

Ship in the chunk order: schema+migration → middleware+rules → admin HTTP →
CLI → frontend → MCP invariant test. Each chunk stays behind a feature flag
(`AGENTBOARD_AUTH_V2=true`) until the frontend lands, so data-plane breakage
during partial rollouts can't happen.
