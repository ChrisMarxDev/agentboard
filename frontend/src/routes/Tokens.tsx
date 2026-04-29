import { useCallback, useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { Copy, KeyRound, Lock, Monitor, Plus, RotateCw, ShieldCheck, Trash2 } from 'lucide-react'
import { useMe } from '../hooks/useMe'
import { apiFetch } from '../lib/session'
import { copyToClipboard } from '../lib/clipboard'
import {
  createTokenForUser,
  listSessionsForUser,
  listTokensForUser,
  revokeSession,
  revokeToken,
  rotateToken,
  type CreatedToken,
  type SessionRow,
  type UserToken,
} from '../lib/auth'

// /tokens — a member's self-serve token page. Uses the scoped
// /api/users/{me}/tokens endpoints; any caller can manage their own
// tokens here without admin elevation. Admins see/manage their own
// tokens here too; they manage other users' tokens via /admin.

const CARD: CSSProperties = {
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: '0.75rem',
  padding: '1.25rem 1.5rem',
}
const LABEL: CSSProperties = {
  fontSize: '0.6875rem',
  fontWeight: 600,
  letterSpacing: '0.06em',
  textTransform: 'uppercase',
  color: 'var(--text-secondary)',
}
const INPUT: CSSProperties = {
  padding: '0.45rem 0.65rem',
  border: '1px solid var(--border)',
  borderRadius: '0.5rem',
  background: 'var(--bg)',
  color: 'var(--text)',
  fontSize: '0.875rem',
  outline: 'none',
}
const BTN: CSSProperties = {
  display: 'inline-flex',
  alignItems: 'center',
  gap: '0.3rem',
  padding: '0.45rem 0.85rem',
  borderRadius: '0.5rem',
  border: '1px solid var(--border)',
  background: 'var(--bg)',
  color: 'var(--text)',
  fontSize: '0.8125rem',
  cursor: 'pointer',
}
const BTN_PRIMARY: CSSProperties = {
  ...BTN,
  background: 'var(--accent)',
  color: 'white',
  border: 'none',
}

export default function Tokens() {
  const me = useMe()
  const [tokens, setTokens] = useState<UserToken[] | null>(null)
  const [reveal, setReveal] = useState<CreatedToken | null>(null)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!me) return
    try {
      setTokens(await listTokensForUser(me.username))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [me])

  useEffect(() => { void refresh() }, [refresh])

  if (!me) {
    return (
      <div style={{ padding: '2rem 1.5rem', color: 'var(--text-secondary)' }}>
        Checking session…
      </div>
    )
  }

  return (
    <div style={{ padding: '2rem 1.5rem', maxWidth: '56rem', margin: '0 auto', color: 'var(--text)' }}>
      <header style={{ marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <KeyRound size={18} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.25rem', fontWeight: 600, margin: 0 }}>Your tokens</h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0.25rem 0 0' }}>
          Signed in as <b>@{me.username}</b>. Tokens are how agents and scripts
          sign in — rotate or revoke any you no longer trust.
        </p>
      </header>

      {error && (
        <div
          style={{
            padding: '0.625rem 0.875rem',
            borderRadius: '0.5rem',
            background: 'color-mix(in srgb, var(--error) 12%, transparent)',
            color: 'var(--error)',
            fontSize: '0.8125rem',
            marginBottom: '1rem',
          }}
        >
          {error}
        </div>
      )}

      {reveal && <RevealBanner token={reveal} onDismiss={() => setReveal(null)} />}

      <section style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '1.25rem' }}>
        <div style={LABEL}>Browser sessions</div>
        <SessionsCard username={me.username} />
      </section>

      <section style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem', marginBottom: '1.25rem' }}>
        <div style={LABEL}>Password</div>
        <ChangePasswordCard username={me.username} />
      </section>

      <section style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={LABEL}>Active tokens</div>
          {!creating && (
            <button style={BTN} onClick={() => setCreating(true)}>
              <Plus size={13} />
              New token
            </button>
          )}
        </div>
        {creating && (
          <NewTokenCard
            username={me.username}
            onCreated={(t) => {
              setReveal(t)
              setCreating(false)
              void refresh()
            }}
            onCancel={() => setCreating(false)}
            onError={setError}
          />
        )}
        {tokens === null ? (
          <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>Loading…</div>
        ) : tokens.length === 0 ? (
          <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.875rem' }}>
            No tokens yet. Mint one above.
          </div>
        ) : (
          tokens
            .filter((t) => !t.revoked_at)
            .map((t) => (
              <TokenRow
                key={t.id}
                token={t}
                username={me.username}
                onRotated={(fresh) => {
                  setReveal(fresh)
                  void refresh()
                }}
                onRevoked={() => void refresh()}
                onError={setError}
              />
            ))
        )}
      </section>
    </div>
  )
}

function NewTokenCard({
  username,
  onCreated,
  onCancel,
  onError,
}: {
  username: string
  onCreated: (t: CreatedToken) => void
  onCancel: () => void
  onError: (msg: string) => void
}) {
  const [label, setLabel] = useState('')
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    try {
      const tok = await createTokenForUser(username, label.trim() || undefined)
      onCreated(tok)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={onSubmit} style={{ ...CARD, display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
      <input
        autoFocus
        value={label}
        onChange={(e) => setLabel(e.target.value)}
        placeholder="Label (e.g. laptop, ci)"
        style={{ ...INPUT, flex: 1 }}
      />
      <button type="submit" style={BTN_PRIMARY} disabled={busy}>
        <Plus size={13} />
        Create
      </button>
      <button type="button" style={BTN} onClick={onCancel} disabled={busy}>
        Cancel
      </button>
    </form>
  )
}

function TokenRow({
  token,
  username,
  onRotated,
  onRevoked,
  onError,
}: {
  token: UserToken
  username: string
  onRotated: (fresh: CreatedToken) => void
  onRevoked: () => void
  onError: (msg: string) => void
}) {
  const [busy, setBusy] = useState(false)

  async function rotate() {
    setBusy(true)
    try {
      const fresh = await rotateToken(username, token.id)
      onRotated(fresh)
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function revoke() {
    if (!confirm(`Revoke token "${token.label || token.id}"? Anyone using it will be signed out.`)) return
    setBusy(true)
    try {
      await revokeToken(username, token.id)
      onRevoked()
    } catch (err) {
      onError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div style={{ ...CARD, display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
      <ShieldCheck size={14} style={{ color: 'var(--text-secondary)' }} />
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: '0.875rem', fontWeight: 500 }}>
          {token.label || <span style={{ color: 'var(--text-secondary)' }}>(no label)</span>}
        </div>
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
          Created {new Date(token.created_at).toLocaleDateString()}
          {token.last_used_at && ` · last used ${new Date(token.last_used_at).toLocaleDateString()}`}
          {token.created_by && ` · by @${token.created_by}`}
        </div>
      </div>
      <button style={BTN} onClick={rotate} disabled={busy} title="Rotate — invalidates the old token">
        <RotateCw size={12} />
        Rotate
      </button>
      <button
        style={{ ...BTN, color: 'var(--error)', borderColor: 'var(--error)' }}
        onClick={revoke}
        disabled={busy}
        title="Revoke"
      >
        <Trash2 size={12} />
        Revoke
      </button>
    </div>
  )
}

// SessionsCard lists every browser session that's ever been minted
// for this user, active first. The "current" row gets a badge so the
// user knows which one they'd be killing if they hit Revoke. Bearer
// tokens are managed in the section below — sessions are exclusively
// the cookie-based browser logins.
function SessionsCard({ username }: { username: string }) {
  const [sessions, setSessions] = useState<SessionRow[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busyID, setBusyID] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setSessions(await listSessionsForUser(username))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [username])

  useEffect(() => { void refresh() }, [refresh])

  if (sessions === null) {
    return <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.8125rem' }}>Loading sessions…</div>
  }
  if (sessions.length === 0) {
    return (
      <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.8125rem' }}>
        No browser sessions yet — sign in via /login to create one.
      </div>
    )
  }

  return (
    <div style={CARD}>
      {error && (
        <div style={{ color: 'var(--error)', fontSize: '0.8125rem', marginBottom: '0.5rem' }}>{error}</div>
      )}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
        {sessions.map((sess) => (
          <div
            key={sess.id}
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr auto',
              alignItems: 'center',
              gap: '0.75rem',
              padding: '0.5rem 0.625rem',
              borderRadius: '0.375rem',
              background: 'var(--bg-secondary)',
              opacity: sess.revoked_at ? 0.5 : 1,
            }}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', minWidth: 0 }}>
              <Monitor size={14} style={{ color: 'var(--text-secondary)', flexShrink: 0 }} />
              <div style={{ minWidth: 0 }}>
                <div style={{ fontSize: '0.8125rem', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {sess.user_agent || 'Unknown browser'}
                  {sess.current && (
                    <span style={{
                      marginLeft: '0.5rem',
                      padding: '0.05rem 0.4rem',
                      borderRadius: '0.25rem',
                      background: 'color-mix(in srgb, var(--accent) 18%, transparent)',
                      color: 'var(--accent)',
                      fontSize: '0.6875rem',
                      fontWeight: 600,
                    }}>this browser</span>
                  )}
                  {sess.revoked_at && (
                    <span style={{
                      marginLeft: '0.5rem',
                      padding: '0.05rem 0.4rem',
                      borderRadius: '0.25rem',
                      background: 'color-mix(in srgb, var(--error) 18%, transparent)',
                      color: 'var(--error)',
                      fontSize: '0.6875rem',
                      fontWeight: 600,
                    }}>revoked</span>
                  )}
                </div>
                <div style={{ fontSize: '0.6875rem', color: 'var(--text-secondary)' }}>
                  {sess.ip ? `${sess.ip} · ` : ''}created {new Date(sess.created_at).toLocaleString()}
                  {sess.last_used_at ? ` · last used ${new Date(sess.last_used_at).toLocaleString()}` : ''}
                </div>
              </div>
            </div>
            {!sess.revoked_at && (
              <button
                style={BTN}
                disabled={busyID === sess.id}
                onClick={async () => {
                  setBusyID(sess.id)
                  try {
                    await revokeSession(username, sess.id)
                    await refresh()
                  } catch (err) {
                    setError(err instanceof Error ? err.message : String(err))
                  } finally {
                    setBusyID(null)
                  }
                }}
              >
                <Trash2 size={12} />
                Revoke
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

// ChangePasswordCard posts to /api/users/{me}/password. Self-scope
// requires the current password as proof-of-possession; admins
// changing their own password are no different from any other user.
function ChangePasswordCard({ username }: { username: string }) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setDone(false)
    if (next !== confirm) {
      setError('New password and confirmation do not match.')
      return
    }
    setBusy(true)
    try {
      const res = await apiFetch(`/api/users/${encodeURIComponent(username)}/password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current_password: current, new_password: next }),
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.error || `HTTP ${res.status}`)
      }
      setCurrent('')
      setNext('')
      setConfirm('')
      setDone(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={onSubmit} style={CARD}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem', marginBottom: '0.625rem' }}>
        <Lock size={14} style={{ color: 'var(--text-secondary)' }} />
        <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)' }}>
          Change the password used at /login. Sessions and tokens stay valid.
        </div>
      </div>
      {error && (
        <div style={{ color: 'var(--error)', fontSize: '0.8125rem', marginBottom: '0.5rem' }}>{error}</div>
      )}
      {done && (
        <div style={{ color: 'var(--accent)', fontSize: '0.8125rem', marginBottom: '0.5rem' }}>
          Password updated.
        </div>
      )}
      <div style={{ display: 'grid', gap: '0.5rem' }}>
        <input
          type="password"
          autoComplete="current-password"
          placeholder="Current password"
          value={current}
          onChange={(e) => setCurrent(e.target.value)}
          style={INPUT}
        />
        <input
          type="password"
          autoComplete="new-password"
          placeholder="New password (min 10 chars)"
          value={next}
          onChange={(e) => setNext(e.target.value)}
          style={INPUT}
        />
        <input
          type="password"
          autoComplete="new-password"
          placeholder="Confirm new password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          style={INPUT}
        />
        <div>
          <button
            type="submit"
            disabled={busy || !current || !next || !confirm}
            style={BTN_PRIMARY}
          >
            {busy ? 'Updating…' : 'Update password'}
          </button>
        </div>
      </div>
    </form>
  )
}

function RevealBanner({ token, onDismiss }: { token: CreatedToken; onDismiss: () => void }) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    if (await copyToClipboard(token.token)) {
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
        marginBottom: '1rem',
      }}
    >
      <div style={{ fontWeight: 600, marginBottom: '0.375rem' }}>
        New token — shown once
      </div>
      <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', marginBottom: '0.75rem' }}>
        Copy this value now. For your security, we never show it again after you
        leave this page.
      </div>
      <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
        <code
          style={{
            flex: 1,
            padding: '0.5rem 0.75rem',
            borderRadius: '0.375rem',
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            fontFamily: 'ui-monospace, monospace',
            fontSize: '0.8125rem',
            overflowX: 'auto',
            whiteSpace: 'nowrap',
          }}
        >
          {token.token}
        </code>
        <button style={BTN} onClick={copy}>
          <Copy size={12} />
          {copied ? 'Copied' : 'Copy'}
        </button>
        <button style={BTN} onClick={onDismiss}>Dismiss</button>
      </div>
    </div>
  )
}
