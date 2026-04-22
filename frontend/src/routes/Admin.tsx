import { useCallback, useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  KeyRound,
  Plus,
  RotateCw,
  ShieldCheck,
  Trash2,
  X,
} from 'lucide-react'
import {
  createTokenForUser,
  createUser,
  deactivateUser,
  fetchMe,
  listUsers,
  revokeToken,
  rotateToken,
  updateUser,
  type AccessMode,
  type CreatedToken,
  type Kind,
  type Me,
  type Rule,
  type User,
  type UserToken,
} from '../lib/auth'
import { clearToken, getToken, redirectToLogin } from '../lib/session'

// Admin page. Rendered inside Layout so the sidebar persists. Uses the
// shared session token — no separate admin-token store. If the user's
// token isn't admin-kind, we show a clear message instead of a prompt.

const CARD: CSSProperties = {
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: '0.75rem',
  padding: '1.25rem 1.5rem',
  boxShadow: '0 1px 2px rgba(0,0,0,0.04), 0 1px 3px rgba(0,0,0,0.06)',
}
const LABEL: CSSProperties = {
  fontSize: '0.6875rem',
  fontWeight: 600,
  letterSpacing: '0.06em',
  textTransform: 'uppercase',
  color: 'var(--text-secondary)',
}
const INPUT: CSSProperties = {
  width: '100%',
  padding: '0.5rem 0.75rem',
  border: '1px solid var(--border)',
  borderRadius: '0.5rem',
  background: 'var(--bg)',
  color: 'var(--text)',
  fontSize: '0.875rem',
  outline: 'none',
}
const BTN_PRIMARY: CSSProperties = {
  padding: '0.5rem 1rem',
  borderRadius: '0.5rem',
  background: 'var(--accent)',
  color: 'white',
  fontSize: '0.875rem',
  fontWeight: 500,
  border: 'none',
  cursor: 'pointer',
}
const BTN_GHOST: CSSProperties = {
  padding: '0.375rem 0.75rem',
  borderRadius: '0.375rem',
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--text)',
  fontSize: '0.8125rem',
  cursor: 'pointer',
}
const BTN_DANGER: CSSProperties = { ...BTN_GHOST, color: 'var(--error)', borderColor: 'var(--error)' }

export default function Admin() {
  const [me, setMe] = useState<Me | null>(null)
  const [state, setState] = useState<'loading' | 'ok' | 'not-admin' | 'no-token'>('loading')

  useEffect(() => {
    // Must have a token; if not, send to login.
    if (!getToken()) {
      redirectToLogin('missing')
      return
    }
    let cancelled = false
    fetchMe()
      .then((m) => {
        if (cancelled) return
        setMe(m)
        setState(m.kind === 'admin' ? 'ok' : 'not-admin')
      })
      .catch(() => {
        // fetchMe 403s when the current token is agent-kind. apiFetch doesn't
        // treat 403 as auth-expired, so we surface it as not-admin. 401
        // would have already been redirected by apiFetch.
        if (cancelled) return
        setState('not-admin')
      })
    return () => { cancelled = true }
  }, [])

  if (state === 'loading') {
    return (
      <Shell>
        <div style={{ color: 'var(--text-secondary)' }}>Checking admin status…</div>
      </Shell>
    )
  }
  if (state === 'no-token') return null // redirected already
  if (state === 'not-admin' || !me) return <NotAdmin />
  return <AdminPanel me={me} />
}

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        padding: '2rem 1.5rem',
        maxWidth: '60rem',
        margin: '0 auto',
        width: '100%',
        color: 'var(--text)',
      }}
    >
      {children}
    </div>
  )
}

function NotAdmin() {
  return (
    <Shell>
      <div style={{ ...CARD, maxWidth: '32rem', margin: '3rem auto 0' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
          <ShieldCheck size={18} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>Admin-only area</h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
          The token you're signed in with is an agent token. User and token
          management requires an admin token. Ask your admin for one, or
          mint one on the host:
        </p>
        <pre
          style={{
            background: 'var(--bg-secondary)',
            padding: '0.75rem',
            borderRadius: '0.375rem',
            fontSize: '0.8125rem',
            overflowX: 'auto',
          }}
        >
          agentboard admin mint-admin &lt;username&gt;
        </pre>
        <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem' }}>
          <button
            onClick={() => {
              clearToken()
              redirectToLogin()
            }}
            style={BTN_GHOST}
          >
            Sign out
          </button>
          <Link to="/" style={{ ...BTN_PRIMARY, textDecoration: 'none', display: 'inline-block' }}>
            Back to dashboard
          </Link>
        </div>
      </div>
    </Shell>
  )
}

// ---------- main panel ----------

function AdminPanel({ me }: { me: Me }) {
  const [users, setUsers] = useState<User[] | null>(null)
  const [reveal, setReveal] = useState<CreatedToken | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [creating, setCreating] = useState<boolean>(false)

  const refresh = useCallback(async () => {
    try {
      const list = await listUsers()
      setUsers(list)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  return (
    <Shell>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '1.5rem' }}>
        <header>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <ShieldCheck size={18} style={{ color: 'var(--accent)' }} />
            <h1 style={{ fontSize: '1.25rem', fontWeight: 600, margin: 0 }}>Auth</h1>
          </div>
          <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0.25rem 0 0' }}>
            Signed in as <b>@{me.username}</b>. Manage users and their
            tokens. Usernames are stable (so @mentions keep working);
            tokens are what you hand to agents.
          </p>
        </header>

        {error && (
          <div
            style={{
              padding: '0.75rem 1rem',
              borderRadius: '0.5rem',
              background: 'color-mix(in srgb, var(--error) 12%, transparent)',
              color: 'var(--error)',
              fontSize: '0.875rem',
            }}
          >
            {error}
          </div>
        )}

        {reveal && <RevealBanner created={reveal} onDismiss={() => setReveal(null)} />}

        <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
            <div style={LABEL}>Users</div>
            {!creating && (
              <button onClick={() => setCreating(true)} style={BTN_GHOST}>
                <Plus size={13} style={{ verticalAlign: '-2px', marginRight: '0.25rem' }} />
                New user
              </button>
            )}
          </div>

          {creating && (
            <NewUserCard
              onCreated={(c) => {
                setReveal(c)
                setCreating(false)
                void refresh()
              }}
              onCancel={() => setCreating(false)}
              onError={setError}
            />
          )}

          <UsersList
            users={users}
            meUsername={me.username}
            onRefresh={refresh}
            onReveal={setReveal}
            onError={setError}
          />
        </section>
      </div>
    </Shell>
  )
}

// ---------- user rows ----------

function UsersList({
  users,
  meUsername,
  onRefresh,
  onReveal,
  onError,
}: {
  users: User[] | null
  meUsername: string
  onRefresh: () => void
  onReveal: (c: CreatedToken) => void
  onError: (msg: string) => void
}) {
  if (users === null) return <div style={{ ...CARD, color: 'var(--text-secondary)' }}>Loading…</div>
  if (users.length === 0)
    return <div style={{ ...CARD, color: 'var(--text-secondary)' }}>No users yet.</div>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
      {users.map((u) => (
        <UserCard
          key={u.username}
          user={u}
          isSelf={u.username === meUsername}
          onRefresh={onRefresh}
          onReveal={onReveal}
          onError={onError}
        />
      ))}
    </div>
  )
}

function UserCard({
  user,
  isSelf,
  onRefresh,
  onReveal,
  onError,
}: {
  user: User
  isSelf: boolean
  onRefresh: () => void
  onReveal: (c: CreatedToken) => void
  onError: (msg: string) => void
}) {
  const [expanded, setExpanded] = useState<boolean>(isSelf)
  const [editing, setEditing] = useState<boolean>(false)
  const deactivated = Boolean(user.deactivated_at)
  const activeTokens = (user.tokens ?? []).filter((t) => !t.revoked_at).length

  async function onDeactivate() {
    if (!window.confirm(`Deactivate @${user.username}? Every token stops working immediately, and the username stays reserved forever.`)) return
    try {
      await deactivateUser(user.username)
      onRefresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  async function onAddToken() {
    const label = window.prompt(`Label for the new token on @${user.username}? (optional)`, '')
    try {
      const created = await createTokenForUser(user.username, label ?? '')
      onReveal(created)
      onRefresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div style={{ ...CARD, padding: 0, opacity: deactivated ? 0.6 : 1 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.75rem',
          padding: '0.875rem 1rem',
          cursor: 'pointer',
        }}
        onClick={() => setExpanded((e) => !e)}
      >
        <Avatar username={user.username} color={user.avatar_color} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontWeight: 500, fontSize: '0.9375rem' }}>@{user.username}</span>
            {user.display_name && (
              <span style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
                {user.display_name}
              </span>
            )}
            <KindPill kind={user.kind} />
            {isSelf && <SelfPill />}
            {deactivated && <DeactivatedPill />}
          </div>
          <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: '0.125rem' }}>
            {deactivated
              ? 'deactivated'
              : `${activeTokens} active token${activeTokens === 1 ? '' : 's'} · ${user.access_mode === 'allow_all' ? 'allow all' : 'restricted'}${user.rules.length ? ` · ${user.rules.length} rule${user.rules.length === 1 ? '' : 's'}` : ''}`}
          </div>
        </div>
        {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
      </div>

      {expanded && (
        <div style={{ borderTop: '1px solid var(--border)', padding: '0.875rem 1rem', background: 'var(--bg-secondary)' }}>
          {editing ? (
            <UserEditForm
              user={user}
              onDone={() => {
                setEditing(false)
                onRefresh()
              }}
              onCancel={() => setEditing(false)}
              onError={onError}
            />
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              <TokensTable
                tokens={user.tokens ?? []}
                username={user.username}
                onRotated={onReveal}
                onChanged={onRefresh}
                onError={onError}
              />
              {!deactivated && (
                <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
                  <button onClick={() => setEditing(true)} style={BTN_GHOST}>Edit</button>
                  <button onClick={onAddToken} style={BTN_GHOST}>
                    <Plus size={13} style={{ verticalAlign: '-2px', marginRight: '0.25rem' }} />
                    Add token
                  </button>
                  {!isSelf && (
                    <button onClick={onDeactivate} style={BTN_DANGER}>Deactivate</button>
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function TokensTable({
  tokens,
  username,
  onRotated,
  onChanged,
  onError,
}: {
  tokens: UserToken[]
  username: string
  onRotated: (c: CreatedToken) => void
  onChanged: () => void
  onError: (msg: string) => void
}) {
  if (tokens.length === 0)
    return <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>No tokens.</div>
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
      {tokens.map((t) => (
        <TokenRow
          key={t.id}
          token={t}
          username={username}
          onRotated={onRotated}
          onChanged={onChanged}
          onError={onError}
        />
      ))}
    </div>
  )
}

function TokenRow({
  token,
  username,
  onRotated,
  onChanged,
  onError,
}: {
  token: UserToken
  username: string
  onRotated: (c: CreatedToken) => void
  onChanged: () => void
  onError: (msg: string) => void
}) {
  const revoked = Boolean(token.revoked_at)
  async function onRotate() {
    try {
      const c = await rotateToken(username, token.id)
      onRotated(c)
      onChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }
  async function onRevoke() {
    if (!window.confirm(`Revoke token "${token.label || 'unnamed'}"? It stops working immediately.`)) return
    try {
      await revokeToken(username, token.id)
      onChanged()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '1fr 1fr auto',
        gap: '0.75rem',
        alignItems: 'center',
        padding: '0.5rem 0.75rem',
        borderRadius: '0.375rem',
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        opacity: revoked ? 0.55 : 1,
      }}
    >
      <div style={{ fontSize: '0.875rem' }}>
        <span style={{ fontWeight: 500 }}>{token.label || 'unnamed'}</span>
        {revoked && <span style={{ marginLeft: '0.5rem', fontSize: '0.75rem', color: 'var(--error)' }}>revoked</span>}
      </div>
      <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
        {token.last_used_at ? `used ${relativeTime(token.last_used_at)}` : 'never used'}
      </div>
      {!revoked && (
        <div style={{ display: 'flex', gap: '0.25rem', justifyContent: 'flex-end' }}>
          <button onClick={onRotate} style={BTN_GHOST} title="Rotate">
            <RotateCw size={12} style={{ verticalAlign: '-1px' }} />
          </button>
          <button onClick={onRevoke} style={BTN_DANGER} title="Revoke">
            <Trash2 size={12} style={{ verticalAlign: '-1px' }} />
          </button>
        </div>
      )}
    </div>
  )
}

function UserEditForm({
  user,
  onDone,
  onCancel,
  onError,
}: {
  user: User
  onDone: () => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  // Username is read-only. To rename, run the CLI escape hatch.
  const [displayName, setDisplayName] = useState(user.display_name ?? '')
  const [mode, setMode] = useState<AccessMode>(user.access_mode)
  const [rulesText, setRulesText] = useState(JSON.stringify(user.rules, null, 2))

  async function save() {
    let rules: Rule[]
    try {
      rules = JSON.parse(rulesText) as Rule[]
      if (!Array.isArray(rules)) throw new Error('rules must be an array')
    } catch (err) {
      onError(`Rules JSON: ${err instanceof Error ? err.message : String(err)}`)
      return
    }
    try {
      await updateUser(user.username, {
        display_name: displayName,
        access_mode: mode,
        rules,
      })
      onDone()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div>
        <div style={LABEL}>Username</div>
        <div
          style={{
            padding: '0.5rem 0.75rem',
            borderRadius: '0.5rem',
            background: 'var(--bg-secondary)',
            color: 'var(--text-secondary)',
            fontFamily: 'ui-monospace, monospace',
            fontSize: '0.875rem',
          }}
          title="Usernames are immutable. To rename, run `agentboard admin rename-user` on the host."
        >
          @{user.username}
        </div>
      </div>
      <div>
        <div style={LABEL}>Display name</div>
        <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} style={INPUT} />
      </div>
      <div>
        <div style={LABEL}>Access mode</div>
        <select value={mode} onChange={(e) => setMode(e.target.value as AccessMode)} style={INPUT}>
          <option value="allow_all">allow_all — blocklist applies</option>
          <option value="restrict_to_list">restrict_to_list — allowlist only</option>
        </select>
      </div>
      <div>
        <div style={LABEL}>Rules (JSON array)</div>
        <textarea
          rows={6}
          value={rulesText}
          onChange={(e) => setRulesText(e.target.value)}
          style={{ ...INPUT, fontFamily: 'ui-monospace, monospace', fontSize: '0.8125rem', resize: 'vertical' }}
        />
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
        <button onClick={onCancel} style={BTN_GHOST}>Cancel</button>
        <button onClick={save} style={BTN_PRIMARY}>Save</button>
      </div>
    </div>
  )
}

function NewUserCard({
  onCreated,
  onCancel,
  onError,
}: {
  onCreated: (c: CreatedToken) => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [kind, setKind] = useState<Kind>('agent')
  const [template, setTemplate] = useState<'full' | 'viewer' | 'custom'>('full')
  const [mode, setMode] = useState<AccessMode>('allow_all')
  const [rulesText, setRulesText] = useState('[]')
  const [busy, setBusy] = useState(false)

  function applyTemplate(t: 'full' | 'viewer' | 'custom') {
    setTemplate(t)
    if (t === 'full') {
      setMode('allow_all')
      setRulesText('[]')
    } else if (t === 'viewer') {
      setMode('restrict_to_list')
      setRulesText(
        JSON.stringify(
          [
            { action: 'allow', pattern: '/api/data/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/content/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/files/**', methods: ['GET'] },
            { action: 'allow', pattern: '/api/events', methods: ['GET'] },
          ] satisfies Rule[],
          null,
          2,
        ),
      )
    }
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const rules: Rule[] = kind === 'admin' ? [] : (JSON.parse(rulesText) as Rule[])
      const created = await createUser({
        username: username.trim().toLowerCase(),
        display_name: displayName.trim() || undefined,
        kind,
        access_mode: kind === 'admin' ? 'allow_all' : mode,
        rules,
      })
      onCreated(created)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={onSubmit} style={CARD}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem' }}>
        <div style={LABEL}>New user</div>
        <button type="button" onClick={onCancel} style={{ ...BTN_GHOST, padding: '0.25rem' }} aria-label="Cancel">
          <X size={14} />
        </button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 9rem', gap: '0.75rem' }}>
        <div>
          <div style={LABEL}>Username</div>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="alice"
            pattern="^[a-z][a-z0-9_-]{0,31}$"
            title="Lowercase letters, digits, _ or -; start with a letter; max 32"
            style={INPUT}
            required
          />
        </div>
        <div>
          <div style={LABEL}>Display name (optional)</div>
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Alice Chen"
            style={INPUT}
          />
        </div>
        <div>
          <div style={LABEL}>Kind</div>
          <select value={kind} onChange={(e) => setKind(e.target.value as Kind)} style={INPUT}>
            <option value="agent">agent</option>
            <option value="admin">admin</option>
          </select>
        </div>
      </div>

      {kind === 'agent' && (
        <>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem', marginTop: '0.75rem' }}>
            <div>
              <div style={LABEL}>Template</div>
              <select
                value={template}
                onChange={(e) => applyTemplate(e.target.value as 'full' | 'viewer' | 'custom')}
                style={INPUT}
              >
                <option value="full">Full access</option>
                <option value="viewer">Read-only viewer</option>
                <option value="custom">Custom</option>
              </select>
            </div>
            <div>
              <div style={LABEL}>Mode</div>
              <select value={mode} onChange={(e) => setMode(e.target.value as AccessMode)} style={INPUT}>
                <option value="allow_all">allow_all</option>
                <option value="restrict_to_list">restrict_to_list</option>
              </select>
            </div>
          </div>
          <div style={{ marginTop: '0.75rem' }}>
            <div style={LABEL}>Rules (JSON array)</div>
            <textarea
              rows={5}
              value={rulesText}
              onChange={(e) => setRulesText(e.target.value)}
              style={{ ...INPUT, fontFamily: 'ui-monospace, monospace', fontSize: '0.8125rem', resize: 'vertical' }}
            />
          </div>
        </>
      )}

      {kind === 'admin' && (
        <p style={{ marginTop: '0.75rem', fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
          Admin users always have full access. Use rule scoping on agents,
          not admins.
        </p>
      )}

      <div style={{ marginTop: '1rem', display: 'flex', justifyContent: 'flex-end' }}>
        <button type="submit" disabled={busy} style={BTN_PRIMARY}>
          <KeyRound size={14} style={{ verticalAlign: '-2px', marginRight: '0.375rem' }} />
          {busy ? 'Creating…' : 'Create + show token'}
        </button>
      </div>
    </form>
  )
}

function Avatar({ username, color }: { username: string; color?: string }) {
  const bg = color ?? 'var(--accent)'
  const initial = username.charAt(0).toUpperCase() || '?'
  return (
    <div
      aria-hidden
      style={{
        width: '2rem',
        height: '2rem',
        borderRadius: '9999px',
        background: bg,
        color: 'white',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        fontWeight: 600,
        fontSize: '0.875rem',
        flexShrink: 0,
      }}
    >
      {initial}
    </div>
  )
}

function KindPill({ kind }: { kind: Kind }) {
  const isAdmin = kind === 'admin'
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: '0.25rem',
        padding: '0.125rem 0.5rem',
        borderRadius: '9999px',
        fontSize: '0.6875rem',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.04em',
        background: isAdmin ? 'var(--accent-light)' : 'var(--bg-secondary)',
        color: isAdmin ? 'var(--accent)' : 'var(--text-secondary)',
      }}
    >
      {isAdmin && <ShieldCheck size={11} />}
      {kind}
    </span>
  )
}

function SelfPill() {
  return (
    <span
      style={{
        fontSize: '0.6875rem',
        padding: '0.125rem 0.375rem',
        borderRadius: '0.25rem',
        background: 'var(--accent-light)',
        color: 'var(--accent)',
        fontWeight: 600,
      }}
    >
      you
    </span>
  )
}

function DeactivatedPill() {
  return (
    <span
      style={{
        fontSize: '0.6875rem',
        padding: '0.125rem 0.375rem',
        borderRadius: '0.25rem',
        background: 'var(--bg-secondary)',
        color: 'var(--text-secondary)',
        fontWeight: 600,
      }}
    >
      deactivated
    </span>
  )
}

function RevealBanner({
  created,
  onDismiss,
}: {
  created: CreatedToken
  onDismiss: () => void
}) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(created.token)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch { /* clipboard blocked */ }
  }
  return (
    <div
      style={{
        ...CARD,
        borderColor: 'var(--warning)',
        background: 'color-mix(in srgb, var(--warning) 8%, var(--bg))',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ fontSize: '0.8125rem', color: 'var(--warning)', fontWeight: 600 }}>
          Token for @{created.username}{created.label ? ` (${created.label})` : ''} — copy it now. It won't be shown again.
        </div>
        <button aria-label="Dismiss" onClick={onDismiss} style={{ ...BTN_GHOST, padding: '0.25rem 0.375rem' }}>
          <X size={14} />
        </button>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: '0.75rem' }}>
        <code
          style={{
            flex: 1,
            padding: '0.5rem 0.75rem',
            borderRadius: '0.375rem',
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            fontFamily: 'ui-monospace, monospace',
            fontSize: '0.8125rem',
            overflowX: 'auto',
            whiteSpace: 'nowrap',
          }}
        >
          {created.token}
        </code>
        <button onClick={copy} style={BTN_GHOST}>
          {copied ? (
            <><Check size={13} style={{ verticalAlign: '-1px', marginRight: '0.25rem' }} /> Copied</>
          ) : (
            <><Copy size={13} style={{ verticalAlign: '-1px', marginRight: '0.25rem' }} /> Copy</>
          )}
        </button>
      </div>
    </div>
  )
}

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime()
  const now = Date.now()
  const s = Math.max(0, Math.round((now - then) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.round(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.round(h / 24)
  return `${d}d ago`
}
