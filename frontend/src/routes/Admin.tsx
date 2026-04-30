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
  createInvitation,
  createLock,
  createTokenForUser,
  createUser,
  deactivateUser,
  deleteLock,
  fetchMe,
  listInvitations,
  listLocks,
  listUsers,
  revokeAllSessionsForUser,
  revokeInvitation,
  revokeToken,
  rotateToken,
  setUserPassword,
  updateUser,
  type AccessMode,
  type CreatedToken,
  type Invitation,
  type Kind,
  type Me,
  type PageLock,
  type Rule,
  type User,
  type UserToken,
} from '../lib/auth'
import { apiFetch, redirectToLogin, signOut } from '../lib/session'
import { copyToClipboard } from '../lib/clipboard'

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
// All button styles use inline-flex so icon + label children align on the
// optical baseline without per-icon verticalAlign hacks.
const BTN_PRIMARY: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: '0.375rem',
  padding: '0.5rem 1rem',
  borderRadius: '0.5rem',
  background: 'var(--accent)',
  color: 'white',
  fontSize: '0.875rem',
  fontWeight: 500,
  lineHeight: 1,
  border: 'none',
  cursor: 'pointer',
}
const BTN_GHOST: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: '0.375rem',
  padding: '0.375rem 0.75rem',
  borderRadius: '0.375rem',
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--text)',
  fontSize: '0.8125rem',
  lineHeight: 1,
  cursor: 'pointer',
}
const BTN_DANGER: CSSProperties = { ...BTN_GHOST, color: 'var(--error)', borderColor: 'var(--error)' }

export default function Admin() {
  const [me, setMe] = useState<Me | null>(null)
  const [state, setState] = useState<'loading' | 'ok' | 'not-admin' | 'no-token'>('loading')

  useEffect(() => {
    // SessionGate higher up the tree already verified there's a
    // signed-in user — fetchMe failing here means the cookie
    // was revoked between gate + admin mount, in which case
    // apiFetch's 401 handler bounces to /login. We only need to
    // distinguish admin vs not-admin from a successful response.
    let cancelled = false
    fetchMe()
      .then((m) => {
        if (cancelled) return
        setMe(m)
        setState(m.kind === 'admin' ? 'ok' : 'not-admin')
      })
      .catch(() => {
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
          The token you're signed in with is not an admin token. User,
          invitation, and lock management requires an admin role. Options:
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
          Ask an admin to send you an invite link, or (operator path){'\n'}
          rm ~/.agentboard/&lt;project&gt;/agentboard.sqlite &amp;&amp; agentboard serve{'\n'}
          # fresh boot re-mints a first-admin /invite URL to stdout
        </pre>
        <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem' }}>
          <button
            onClick={async () => {
              await signOut()
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
                <Plus size={14} />
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

        <SharesSection onError={setError} />

        <WebhooksSection onError={setError} />

        <TeamsSection onError={setError} />

        <InvitationsSection onError={setError} />

        <LocksSection onError={setError} />
      </div>
    </Shell>
  )
}

interface AdminWebhook {
  id: string
  event_pattern: string
  destination_url: string
  label?: string
  created_by: string
  created_at: string
  revoked_at?: string
  last_attempt_at?: string
  last_success_at?: string
  last_status: 'pending' | 'ok' | 'retrying' | 'dead_lettered'
  last_status_code: number
  last_error?: string
  failure_count: number
  success_count: number
}

function WebhooksSection({ onError }: { onError: (msg: string | null) => void }) {
  const [subs, setSubs] = useState<AdminWebhook[] | null>(null)
  const [busyID, setBusyID] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await apiFetch('/api/admin/webhooks')
      if (!res.ok) throw new Error(`GET /api/admin/webhooks → ${res.status}`)
      setSubs((await res.json()) as AdminWebhook[])
      onError(null)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }, [onError])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const revoke = useCallback(
    async (id: string) => {
      if (!confirm('Revoke this webhook? Deliveries stop immediately.')) return
      setBusyID(id)
      try {
        const res = await apiFetch(`/api/webhooks/${id}`, { method: 'DELETE' })
        if (!res.ok) throw new Error(`revoke → ${res.status}`)
        await refresh()
      } catch (err) {
        onError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusyID(null)
      }
    },
    [refresh, onError],
  )

  const test = useCallback(
    async (id: string) => {
      setBusyID(id)
      try {
        const res = await apiFetch(`/api/webhooks/${id}/test`, { method: 'POST' })
        const body = (await res.json()) as { status_code?: number; error?: string }
        const msg =
          body.error != null
            ? `Test delivery failed: ${body.error}`
            : `Test delivery returned HTTP ${body.status_code}`
        alert(msg)
        await refresh()
      } catch (err) {
        onError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusyID(null)
      }
    },
    [refresh, onError],
  )

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={LABEL}>Webhook subscriptions</div>

      {subs === null ? (
        <div style={{ fontSize: '0.875rem', color: 'var(--text-secondary)' }}>Loading…</div>
      ) : subs.length === 0 ? (
        <div
          style={{
            padding: '1rem',
            borderRadius: '0.5rem',
            border: '1px dashed var(--border)',
            fontSize: '0.875rem',
            color: 'var(--text-secondary)',
          }}
        >
          No webhooks configured. Agents can register one with{' '}
          <code>POST /api/webhooks</code>.
        </div>
      ) : (
        <div
          style={{
            border: '1px solid var(--border)',
            borderRadius: '0.5rem',
            overflow: 'hidden',
            fontSize: '0.875rem',
          }}
        >
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ background: 'var(--bg-secondary)', textAlign: 'left' }}>
                <th style={TH}>Pattern</th>
                <th style={TH}>Destination</th>
                <th style={TH}>Label</th>
                <th style={TH}>Status</th>
                <th style={TH}>Counts</th>
                <th style={TH}>Created by</th>
                <th style={TH}></th>
              </tr>
            </thead>
            <tbody>
              {subs.map(sub => {
                const isRevoked = Boolean(sub.revoked_at)
                const statusColor =
                  sub.last_status === 'ok'
                    ? 'rgb(22, 163, 74)'
                    : sub.last_status === 'dead_lettered'
                    ? 'var(--error)'
                    : sub.last_status === 'retrying'
                    ? '#d97706'
                    : 'var(--text-secondary)'
                return (
                  <tr
                    key={sub.id}
                    style={{
                      borderTop: '1px solid var(--border)',
                      opacity: isRevoked ? 0.55 : 1,
                    }}
                  >
                    <td style={TD}>
                      <code>{sub.event_pattern}</code>
                    </td>
                    <td style={{ ...TD, color: 'var(--text-secondary)', wordBreak: 'break-all' }}>
                      {sub.destination_url}
                    </td>
                    <td style={{ ...TD, color: 'var(--text-secondary)' }}>
                      {sub.label || <em style={{ opacity: 0.6 }}>—</em>}
                    </td>
                    <td style={{ ...TD, color: statusColor, fontWeight: 500 }}>
                      {isRevoked ? 'revoked' : sub.last_status}
                      {sub.last_status_code ? ` (${sub.last_status_code})` : ''}
                    </td>
                    <td style={TD}>
                      <span style={{ color: 'rgb(22, 163, 74)' }}>✓ {sub.success_count}</span>
                      {' · '}
                      <span style={{ color: 'var(--error)' }}>✗ {sub.failure_count}</span>
                    </td>
                    <td style={TD}>@{sub.created_by}</td>
                    <td style={{ ...TD, textAlign: 'right', whiteSpace: 'nowrap' }}>
                      {!isRevoked && (
                        <>
                          <button
                            type="button"
                            disabled={busyID === sub.id}
                            onClick={() => void test(sub.id)}
                            style={{
                              fontSize: '0.75rem',
                              padding: '0.25rem 0.5rem',
                              borderRadius: '0.375rem',
                              border: '1px solid var(--border)',
                              background: 'transparent',
                              color: 'var(--text)',
                              cursor: busyID === sub.id ? 'not-allowed' : 'pointer',
                              marginRight: 4,
                            }}
                          >
                            Test
                          </button>
                          <button
                            type="button"
                            disabled={busyID === sub.id}
                            onClick={() => void revoke(sub.id)}
                            style={{
                              fontSize: '0.75rem',
                              padding: '0.25rem 0.5rem',
                              borderRadius: '0.375rem',
                              border: '1px solid var(--border)',
                              background: 'transparent',
                              color: 'var(--error)',
                              cursor: busyID === sub.id ? 'not-allowed' : 'pointer',
                            }}
                          >
                            Revoke
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

interface AdminShare {
  id: string
  path: string
  created_by: string
  created_at: string
  expires_at?: string
  last_used_at?: string
  use_count: number
  label?: string
  max_uses?: number
  expired?: boolean
}

function SharesSection({ onError }: { onError: (msg: string | null) => void }) {
  const [shares, setShares] = useState<AdminShare[] | null>(null)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const res = await apiFetch('/api/admin/shares')
      if (!res.ok) throw new Error(`GET /api/admin/shares → ${res.status}`)
      setShares((await res.json()) as AdminShare[])
      onError(null)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }, [onError])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const revoke = useCallback(
    async (id: string) => {
      if (!confirm('Revoke this share? Anyone holding the link will be locked out immediately.')) return
      setBusy(true)
      try {
        const res = await apiFetch(`/api/share/${id}`, { method: 'DELETE' })
        if (!res.ok) throw new Error(`revoke → ${res.status}`)
        await refresh()
      } catch (err) {
        onError(err instanceof Error ? err.message : String(err))
      } finally {
        setBusy(false)
      }
    },
    [refresh, onError],
  )

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={LABEL}>Active share links</div>

      {shares === null ? (
        <div style={{ fontSize: '0.875rem', color: 'var(--text-secondary)' }}>Loading…</div>
      ) : shares.length === 0 ? (
        <div
          style={{
            padding: '1rem',
            borderRadius: '0.5rem',
            border: '1px dashed var(--border)',
            fontSize: '0.875rem',
            color: 'var(--text-secondary)',
          }}
        >
          No active shares. Use the &quot;Share publicly…&quot; action on any page to create one.
        </div>
      ) : (
        <div
          style={{
            border: '1px solid var(--border)',
            borderRadius: '0.5rem',
            overflow: 'hidden',
            fontSize: '0.875rem',
          }}
        >
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <thead>
              <tr style={{ background: 'var(--bg-secondary)', textAlign: 'left' }}>
                <th style={TH}>Page</th>
                <th style={TH}>Label</th>
                <th style={TH}>Created by</th>
                <th style={TH}>Uses</th>
                <th style={TH}>Expires</th>
                <th style={TH}></th>
              </tr>
            </thead>
            <tbody>
              {shares.map(s => (
                <tr key={s.id} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={TD}>
                    <a href={s.path} style={{ color: 'var(--accent)' }}>
                      {s.path}
                    </a>
                  </td>
                  <td style={{ ...TD, color: 'var(--text-secondary)' }}>
                    {s.label || <em style={{ opacity: 0.6 }}>—</em>}
                  </td>
                  <td style={TD}>@{s.created_by}</td>
                  <td style={TD}>
                    {s.use_count}
                    {s.max_uses ? ` / ${s.max_uses}` : ''}
                  </td>
                  <td style={{ ...TD, color: s.expired ? 'var(--error)' : 'var(--text)' }}>
                    {s.expires_at ? new Date(s.expires_at).toLocaleString() : 'never'}
                    {s.expired ? ' (expired)' : ''}
                  </td>
                  <td style={{ ...TD, textAlign: 'right' }}>
                    <button
                      type="button"
                      disabled={busy}
                      onClick={() => void revoke(s.id)}
                      style={{
                        fontSize: '0.75rem',
                        padding: '0.25rem 0.5rem',
                        borderRadius: '0.375rem',
                        border: '1px solid var(--border)',
                        background: 'transparent',
                        color: 'var(--error)',
                        cursor: busy ? 'not-allowed' : 'pointer',
                      }}
                    >
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

const TH: React.CSSProperties = {
  padding: '0.5rem 0.75rem',
  fontSize: '0.7rem',
  fontWeight: 600,
  textTransform: 'uppercase',
  letterSpacing: '0.04em',
  color: 'var(--text-secondary)',
}
const TD: React.CSSProperties = { padding: '0.5rem 0.75rem', verticalAlign: 'top' }

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

  async function onSetPassword() {
    const pw = window.prompt(
      `Set a new browser password for @${user.username}.\n` +
        `Min 10 characters. Existing sessions stay valid until you revoke them separately.`,
      '',
    )
    if (pw === null || pw === '') return
    if (pw.length < 10) {
      onError('Password must be at least 10 characters.')
      return
    }
    try {
      await setUserPassword(user.username, pw)
      window.alert(`Password updated for @${user.username}.`)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  async function onRevokeAllSessions() {
    if (!window.confirm(
      `Revoke every active browser session for @${user.username}? ` +
      `Bearer tokens are NOT touched.`,
    )) return
    try {
      const n = await revokeAllSessionsForUser(user.username)
      window.alert(`Revoked ${n} session(s) for @${user.username}.`)
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
                <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                  <button onClick={() => setEditing(true)} style={BTN_GHOST}>Edit</button>
                  <button onClick={onAddToken} style={BTN_GHOST}>
                    <Plus size={14} />
                    Add token
                  </button>
                  <button onClick={onSetPassword} style={BTN_GHOST} title="Force-set the browser password for this user">
                    Set password
                  </button>
                  <button onClick={onRevokeAllSessions} style={BTN_GHOST} title="Revoke every active browser session for this user">
                    Revoke sessions
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
          <button onClick={onRotate} style={BTN_GHOST} title="Rotate" aria-label="Rotate token">
            <RotateCw size={13} />
          </button>
          <button onClick={onRevoke} style={BTN_DANGER} title="Revoke" aria-label="Revoke token">
            <Trash2 size={13} />
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
  const [kind, setKind] = useState<Kind>('member')
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
            { action: 'allow', pattern: '/api/**', methods: ['GET'] },
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
          <div style={LABEL}>Role</div>
          <select value={kind} onChange={(e) => setKind(e.target.value as Kind)} style={INPUT}>
            <option value="member">member</option>
            <option value="bot">bot</option>
            <option value="admin">admin</option>
          </select>
        </div>
      </div>

      {kind !== 'admin' && (
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
          <KeyRound size={14} />
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
    if (await copyToClipboard(created.token)) {
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    }
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
          {copied ? <><Check size={13} /> Copied</> : <><Copy size={13} /> Copy</>}
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

// ---------- teams ----------

interface AdminTeam {
  slug: string
  display_name?: string
  description?: string
  created_at: string
  created_by?: string
  members?: { username: string; role?: string; added_at: string }[]
}

function TeamsSection({ onError }: { onError: (msg: string | null) => void }) {
  const [teams, setTeams] = useState<AdminTeam[] | null>(null)
  const [creating, setCreating] = useState(false)
  const [busySlug, setBusySlug] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const res = await apiFetch('/api/teams')
      if (!res.ok) throw new Error(`list teams: ${res.status}`)
      const body = (await res.json()) as { teams?: AdminTeam[] }
      setTeams(body.teams ?? [])
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }, [onError])
  useEffect(() => { void refresh() }, [refresh])

  const createTeam = async (slug: string, displayName: string, description: string) => {
    setBusySlug(slug)
    try {
      const res = await apiFetch('/api/admin/teams', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug, display_name: displayName, description }),
      })
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(body.error ?? `create: ${res.status}`)
      }
      setCreating(false)
      await refresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusySlug(null)
    }
  }

  const deleteTeam = async (slug: string) => {
    if (!confirm(`Delete team @${slug}?`)) return
    setBusySlug(slug)
    try {
      const res = await apiFetch(`/api/admin/teams/${slug}`, { method: 'DELETE' })
      if (!res.ok) throw new Error(`delete: ${res.status}`)
      await refresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusySlug(null)
    }
  }

  const addMember = async (slug: string, username: string, role: string) => {
    setBusySlug(slug)
    try {
      const res = await apiFetch(`/api/admin/teams/${slug}/members`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, role }),
      })
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(body.error ?? `add member: ${res.status}`)
      }
      await refresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusySlug(null)
    }
  }

  const removeMember = async (slug: string, username: string) => {
    setBusySlug(slug)
    try {
      const res = await apiFetch(`/api/admin/teams/${slug}/members/${username}`, {
        method: 'DELETE',
      })
      if (!res.ok) throw new Error(`remove member: ${res.status}`)
      await refresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusySlug(null)
    }
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={LABEL}>Teams</div>
        {!creating && (
          <button onClick={() => setCreating(true)} style={BTN_GHOST}>
            <Plus size={14} />
            New team
          </button>
        )}
      </div>
      {creating && (
        <NewTeamCard
          busy={busySlug !== null}
          onCreate={createTeam}
          onCancel={() => setCreating(false)}
        />
      )}
      {teams === null && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
          Loading teams…
        </div>
      )}
      {teams && teams.length === 0 && !creating && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
          No teams yet. @mentions of a team name expand to every member's inbox.
        </div>
      )}
      {teams?.map(t => (
        <TeamCard
          key={t.slug}
          team={t}
          busy={busySlug === t.slug}
          onDelete={() => deleteTeam(t.slug)}
          onAddMember={(u, r) => addMember(t.slug, u, r)}
          onRemoveMember={u => removeMember(t.slug, u)}
        />
      ))}
    </section>
  )
}

function NewTeamCard({
  busy,
  onCreate,
  onCancel,
}: {
  busy: boolean
  onCreate: (slug: string, displayName: string, description: string) => void
  onCancel: () => void
}) {
  const [slug, setSlug] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [description, setDescription] = useState('')

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!slug) return
    onCreate(slug.trim().toLowerCase(), displayName.trim(), description.trim())
  }

  return (
    <form onSubmit={submit} style={{ ...CARD, display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '0.75rem' }}>
        <div>
          <div style={LABEL}>Slug</div>
          <input
            autoFocus
            value={slug}
            onChange={e => setSlug(e.target.value)}
            placeholder="marketing"
            style={INPUT}
            required
            pattern="[a-z][a-z0-9_\-]*"
            maxLength={32}
          />
        </div>
        <div>
          <div style={LABEL}>Display name</div>
          <input
            value={displayName}
            onChange={e => setDisplayName(e.target.value)}
            placeholder="Marketing"
            style={INPUT}
          />
        </div>
      </div>
      <div>
        <div style={LABEL}>Description</div>
        <input
          value={description}
          onChange={e => setDescription(e.target.value)}
          placeholder="Optional — one-line summary"
          style={INPUT}
        />
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onCancel} style={BTN_GHOST} disabled={busy}>
          Cancel
        </button>
        <button type="submit" style={BTN_PRIMARY} disabled={busy || !slug}>
          Create team
        </button>
      </div>
    </form>
  )
}

function TeamCard({
  team,
  busy,
  onDelete,
  onAddMember,
  onRemoveMember,
}: {
  team: AdminTeam
  busy: boolean
  onDelete: () => void
  onAddMember: (username: string, role: string) => void
  onRemoveMember: (username: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [addUser, setAddUser] = useState('')
  const [addRole, setAddRole] = useState('')

  const memberCount = team.members?.length ?? 0

  return (
    <div style={CARD}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
        <button
          onClick={() => setExpanded(v => !v)}
          style={{ background: 'transparent', border: 'none', cursor: 'pointer', color: 'var(--text)' }}
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </button>
        <div style={{ fontWeight: 600 }}>@{team.slug}</div>
        {team.display_name && (
          <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
            · {team.display_name}
          </div>
        )}
        <div style={{ marginLeft: 'auto', fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
          {memberCount} member{memberCount === 1 ? '' : 's'}
        </div>
        <button
          onClick={onDelete}
          disabled={busy}
          style={{ ...BTN_GHOST, color: 'var(--error)', borderColor: 'var(--error)' }}
          aria-label="Delete team"
          title="Delete team"
        >
          <Trash2 size={13} />
        </button>
      </div>
      {team.description && (
        <div style={{ marginTop: '0.5rem', fontSize: '0.875rem', color: 'var(--text-secondary)' }}>
          {team.description}
        </div>
      )}
      {expanded && (
        <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
          {(team.members ?? []).map(m => (
            <div
              key={m.username}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.5rem',
                padding: '0.375rem 0.625rem',
                background: 'var(--bg-secondary)',
                borderRadius: '0.375rem',
              }}
            >
              <span style={{ fontSize: '0.875rem' }}>@{m.username}</span>
              {m.role && (
                <span
                  style={{
                    fontSize: '0.6875rem',
                    color: 'var(--text-secondary)',
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                  }}
                >
                  {m.role}
                </span>
              )}
              <button
                onClick={() => onRemoveMember(m.username)}
                style={{ ...BTN_GHOST, marginLeft: 'auto' }}
                disabled={busy}
                aria-label={`Remove ${m.username}`}
              >
                <X size={12} />
              </button>
            </div>
          ))}
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', paddingTop: '0.25rem' }}>
            <input
              value={addUser}
              onChange={e => setAddUser(e.target.value)}
              placeholder="username"
              style={{ ...INPUT, flex: 1 }}
            />
            <input
              value={addRole}
              onChange={e => setAddRole(e.target.value)}
              placeholder="role (optional)"
              style={{ ...INPUT, width: '12rem' }}
            />
            <button
              onClick={() => {
                if (!addUser) return
                onAddMember(addUser.trim().toLowerCase(), addRole.trim())
                setAddUser('')
                setAddRole('')
              }}
              style={BTN_PRIMARY}
              disabled={busy || !addUser}
            >
              <Plus size={13} />
              Add
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------- invitations ----------

function InvitationsSection({ onError }: { onError: (msg: string | null) => void }) {
  const [invites, setInvites] = useState<Invitation[] | null>(null)
  const [creating, setCreating] = useState(false)
  const [reveal, setReveal] = useState<Invitation | null>(null)

  const refresh = useCallback(async () => {
    try {
      setInvites(await listInvitations())
      onError(null)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }, [onError])
  useEffect(() => { void refresh() }, [refresh])

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={LABEL}>Invitations</div>
        {!creating && (
          <button onClick={() => setCreating(true)} style={BTN_GHOST}>
            <Plus size={14} />
            New invite
          </button>
        )}
      </div>
      {creating && (
        <NewInviteCard
          onCreated={(inv) => {
            setReveal(inv)
            setCreating(false)
            void refresh()
          }}
          onCancel={() => setCreating(false)}
          onError={onError}
        />
      )}
      {reveal && <InviteReveal invite={reveal} onDismiss={() => setReveal(null)} />}
      {invites === null && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
          Loading invitations…
        </div>
      )}
      {invites && invites.length === 0 && !creating && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
          No invitations. Create one to onboard a new user without CLI access.
        </div>
      )}
      {invites?.map((inv) => (
        <InvitationRow
          key={inv.id}
          invite={inv}
          onRevoke={async () => {
            if (!confirm(`Revoke invitation ${inv.id}?`)) return
            try {
              await revokeInvitation(inv.id)
              await refresh()
            } catch (err) {
              onError(err instanceof Error ? err.message : String(err))
            }
          }}
        />
      ))}
    </section>
  )
}

function NewInviteCard({
  onCreated,
  onCancel,
  onError,
}: {
  onCreated: (inv: Invitation) => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  const [role, setRole] = useState<Kind>('member')
  const [label, setLabel] = useState('')
  const [expiresInDays, setExpiresInDays] = useState(7)
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const inv = await createInvitation({ role, label: label.trim() || undefined, expires_in_days: expiresInDays })
      onCreated(inv)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} style={{ ...CARD, display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 120px', gap: '0.75rem' }}>
        <div>
          <div style={LABEL}>Role</div>
          <select value={role} onChange={(e) => setRole(e.target.value as Kind)} style={INPUT}>
            <option value="member">member</option>
            <option value="bot">bot</option>
            <option value="admin">admin</option>
          </select>
        </div>
        <div>
          <div style={LABEL}>Label</div>
          <input
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="Design team onboard"
            style={INPUT}
          />
        </div>
        <div>
          <div style={LABEL}>Expires in</div>
          <select value={expiresInDays} onChange={(e) => setExpiresInDays(Number(e.target.value))} style={INPUT}>
            <option value={1}>1 day</option>
            <option value={7}>7 days</option>
            <option value={14}>14 days</option>
            <option value={30}>30 days</option>
          </select>
        </div>
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onCancel} style={BTN_GHOST} disabled={busy}>Cancel</button>
        <button type="submit" style={BTN_PRIMARY} disabled={busy}>
          Create invitation
        </button>
      </div>
    </form>
  )
}

function InviteReveal({ invite, onDismiss }: { invite: Invitation; onDismiss: () => void }) {
  const url = `${window.location.origin}/invite/${invite.id}`
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (await copyToClipboard(url)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    }
  }
  return (
    <div
      style={{
        ...CARD,
        background: 'color-mix(in srgb, var(--accent) 6%, transparent)',
        border: '1px solid color-mix(in srgb, var(--accent) 40%, transparent)',
      }}
    >
      <div style={{ fontWeight: 600 }}>Invite created — share this URL</div>
      <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', margin: '0.25rem 0 0.625rem' }}>
        Role: <b>{invite.role}</b> · Expires {new Date(invite.expires_at).toLocaleDateString()}
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
        <code
          style={{
            flex: 1,
            padding: '0.4rem 0.65rem',
            borderRadius: '0.375rem',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            fontSize: '0.8125rem',
            overflowX: 'auto',
            whiteSpace: 'nowrap',
          }}
        >
          {url}
        </code>
        <button style={BTN_GHOST} onClick={copy}>
          {copied ? <><Check size={13} /> Copied</> : <><Copy size={13} /> Copy</>}
        </button>
        <button style={BTN_GHOST} onClick={onDismiss}>Dismiss</button>
      </div>
    </div>
  )
}

function InvitationRow({ invite, onRevoke }: { invite: Invitation; onRevoke: () => void }) {
  const status = invite.status
  const color = status === 'active'
    ? 'var(--success)'
    : status === 'expired' || status === 'redeemed'
      ? 'var(--text-secondary)'
      : 'var(--error)'
  return (
    <div style={{ ...CARD, display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
      <div style={{ flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', fontSize: '0.875rem' }}>
          <code style={{ fontSize: '0.8125rem' }}>{invite.id}</code>
          <span style={{ color, textTransform: 'uppercase', fontSize: '0.6875rem', letterSpacing: '0.04em', fontWeight: 600 }}>
            {status}
          </span>
          <span style={{ color: 'var(--text-secondary)', fontSize: '0.75rem' }}>
            {invite.role}
            {invite.label && ` · ${invite.label}`}
          </span>
        </div>
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: 2 }}>
          by @{invite.created_by} · expires {new Date(invite.expires_at).toLocaleDateString()}
          {invite.redeemed_by && ` · claimed by @${invite.redeemed_by}`}
        </div>
      </div>
      {status === 'active' && (
        <button onClick={onRevoke} style={{ ...BTN_GHOST, color: 'var(--error)', borderColor: 'var(--error)' }}>
          <X size={12} />
          Revoke
        </button>
      )}
    </div>
  )
}

// ---------- page locks ----------

function LocksSection({ onError }: { onError: (msg: string | null) => void }) {
  const [locks, setLocks] = useState<PageLock[] | null>(null)
  const [creating, setCreating] = useState(false)

  const refresh = useCallback(async () => {
    try {
      setLocks(await listLocks())
      onError(null)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }, [onError])
  useEffect(() => { void refresh() }, [refresh])

  const remove = async (path: string) => {
    if (!confirm(`Unlock ${path}? Members will be able to edit it again.`)) return
    try {
      await deleteLock(path)
      await refresh()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={LABEL}>Locked pages</div>
        {!creating && (
          <button onClick={() => setCreating(true)} style={BTN_GHOST}>
            <Plus size={14} />
            Lock a page
          </button>
        )}
      </div>
      {creating && (
        <NewLockCard
          onCreated={() => {
            setCreating(false)
            void refresh()
          }}
          onCancel={() => setCreating(false)}
          onError={onError}
        />
      )}
      {locks === null && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>Loading locks…</div>
      )}
      {locks && locks.length === 0 && !creating && (
        <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
          No locked pages. Admins can also lock any page from the page actions menu.
        </div>
      )}
      {locks?.map((l) => (
        <div key={l.path} style={{ ...CARD, display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: '0.875rem', fontWeight: 500 }}>
              <Link to={'/' + l.path} style={{ color: 'var(--text)', textDecoration: 'none' }}>/{l.path}</Link>
            </div>
            <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginTop: 2 }}>
              by @{l.locked_by} · {new Date(l.locked_at).toLocaleString()}
              {l.reason && ` · ${l.reason}`}
            </div>
          </div>
          <button onClick={() => remove(l.path)} style={{ ...BTN_GHOST, color: 'var(--error)', borderColor: 'var(--error)' }}>
            <X size={12} />
            Unlock
          </button>
        </div>
      ))}
    </section>
  )
}

function NewLockCard({
  onCreated,
  onCancel,
  onError,
}: {
  onCreated: () => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  const [path, setPath] = useState('')
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!path.trim()) return
    setBusy(true)
    try {
      await createLock(path.trim(), reason.trim() || undefined)
      onCreated()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} style={{ ...CARD, display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
      <div>
        <div style={LABEL}>Page path</div>
        <input
          autoFocus
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="handbook/onboarding"
          style={INPUT}
          required
        />
      </div>
      <div>
        <div style={LABEL}>Reason (optional)</div>
        <input
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder="canonical doc — contact Alice for changes"
          style={INPUT}
        />
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
        <button type="button" onClick={onCancel} style={BTN_GHOST} disabled={busy}>Cancel</button>
        <button type="submit" style={BTN_PRIMARY} disabled={busy || !path.trim()}>
          Lock page
        </button>
      </div>
    </form>
  )
}
