import { useState, useEffect, type CSSProperties, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'
import { apiFetch, setToken, type SessionUser } from '../lib/session'

// /login — paste a token, store it, go where you were going.
//
// This is the single sign-in surface. Admin + agent tokens both go
// through here. Admin capability flows from the token's `kind`; the UI
// doesn't ask which kind.
//
// On submit we call GET /api/admin/me with the pasted token:
//   - 200 → admin token, valid, sign in.
//   - 403 → valid token but agent-kind. We sign in anyway; the pasted
//     token gets used for the dashboard, /admin will show its own hint.
//   - 401 → invalid token. Surface the error.
// We use /api/admin/me instead of a dedicated verify endpoint because it
// conveniently answers "is this a live token" with one call; the 403
// branch tells us agent-kind without a second request.

const CARD: CSSProperties = {
  background: 'var(--bg)',
  border: '1px solid var(--border)',
  borderRadius: '0.75rem',
  padding: '1.5rem',
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

export default function Login() {
  const [search] = useSearchParams()
  const next = search.get('next') || '/'
  const reason = search.get('reason')

  const [token, setTokenInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  // If the user just got bounced here by a 401, surface that.
  const expired = reason === 'expired'

  useEffect(() => {
    document.title = 'Sign in — AgentBoard'
  }, [])

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmed = token.trim()
    if (!trimmed) return
    setBusy(true)
    setError(null)
    try {
      // Verify the token by asking /api/admin/me. 200 = admin, 403 = valid
      // agent token, 401 = bad token.
      const res = await apiFetch('/api/admin/me', {
        skipAuth: true,
        headers: { Authorization: `Bearer ${trimmed}` },
      })
      if (res.status === 200) {
        const me = (await res.json()) as SessionUser
        setToken(trimmed)
        // Sanity: hit a data-plane endpoint next — if it 403s the token
        // has zero rules; we still sign in and let the dashboard show
        // what it will.
        void me // admin fine
        window.location.assign(next)
        return
      }
      if (res.status === 403) {
        // Valid token, not admin. Sign in anyway; dashboard works.
        setToken(trimmed)
        window.location.assign(next)
        return
      }
      if (res.status === 401) {
        setError('That token is invalid, revoked, or the user was deactivated.')
        setBusy(false)
        return
      }
      setError(`Unexpected response: ${res.status}`)
      setBusy(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setBusy(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: 'var(--bg-secondary)',
        color: 'var(--text)',
        padding: '1rem',
      }}
    >
      <div style={{ ...CARD, width: '100%', maxWidth: '28rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
          <ShieldCheck size={18} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>Sign in to AgentBoard</h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
          Paste your access token. The admin who created your user handed
          you one; the token your browser used to remember is also valid.
        </p>

        {expired && (
          <div
            style={{
              marginBottom: '0.75rem',
              padding: '0.5rem 0.75rem',
              borderRadius: '0.375rem',
              background: 'color-mix(in srgb, var(--warning) 14%, transparent)',
              color: 'var(--warning)',
              fontSize: '0.8125rem',
            }}
          >
            Your session ended. Paste your token to continue.
          </div>
        )}

        <form onSubmit={onSubmit}>
          <label style={{ ...LABEL, display: 'block', marginBottom: '0.375rem' }}>Token</label>
          <input
            autoFocus
            type="password"
            value={token}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder="ab_…"
            style={{ ...INPUT, fontFamily: 'ui-monospace, monospace' }}
          />
          {error && (
            <div
              style={{
                marginTop: '0.75rem',
                padding: '0.5rem 0.75rem',
                borderRadius: '0.375rem',
                background: 'color-mix(in srgb, var(--error) 12%, transparent)',
                color: 'var(--error)',
                fontSize: '0.8125rem',
              }}
            >
              {error}
            </div>
          )}
          <div style={{ marginTop: '1rem', display: 'flex', justifyContent: 'flex-end' }}>
            <button type="submit" disabled={busy || !token.trim()} style={BTN_PRIMARY}>
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </div>
        </form>

        <p style={{ marginTop: '1rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
          First time? The installer prints an admin token on first boot of
          the server. Run <code style={{ background: 'var(--bg-secondary)', padding: '0.1rem 0.3rem', borderRadius: '0.25rem' }}>agentboard admin mint-admin &lt;name&gt;</code> on the host to mint a new one.
        </p>
      </div>
    </div>
  )
}
