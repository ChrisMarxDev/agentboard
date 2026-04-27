import { useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ShieldCheck } from 'lucide-react'
import { apiFetch, fetchSetupStatus, setToken } from '../lib/session'

// /login — the token-paste surface.
//
// When the board is unclaimed AND an active first-admin invitation
// exists, we show a hint linking to /invite/<id>. Otherwise the only
// motion is: paste a token + click Sign in. Token validation calls
// /api/me (moved from /api/admin/me so members + bots work too).

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

function Page({ children }: { children: React.ReactNode }) {
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
      <div style={{ ...CARD, width: '100%', maxWidth: '28rem' }}>{children}</div>
    </div>
  )
}

// Extended setup-status shape (v1 adds invite_url).
interface SetupStatus {
  initialized: boolean
  invite_url?: string
}

export default function Login() {
  const [search] = useSearchParams()
  const next = search.get('next') || '/'
  const reason = search.get('reason')
  const [status, setStatus] = useState<SetupStatus | null>(null)

  useEffect(() => {
    document.title = 'Sign in — AgentBoard'
    fetchSetupStatus().then(setStatus).catch(() => setStatus({ initialized: true }))
  }, [])

  if (!status) {
    return (
      <Page>
        <div style={{ color: 'var(--text-secondary)' }}>Checking board state…</div>
      </Page>
    )
  }

  return <SignInForm next={next} reason={reason} status={status} />
}

function SignInForm({
  next,
  reason,
  status,
}: {
  next: string
  reason: string | null
  status: SetupStatus
}) {
  const [token, setTokenInput] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const expired = reason === 'expired'

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmed = token.trim()
    if (!trimmed) return
    setBusy(true)
    setError(null)
    try {
      const res = await apiFetch('/api/me', {
        skipAuth: true,
        headers: { Authorization: `Bearer ${trimmed}` },
      })
      if (res.ok) {
        setToken(trimmed)
        window.location.assign(next)
        return
      }
      if (res.status === 401) {
        setError('That token is invalid, revoked, or the user was deactivated.')
      } else if (res.status === 403) {
        // Edge: token valid but user was deactivated. Treat like 401.
        setError('That token is no longer allowed.')
      } else {
        setError(`Unexpected response: ${res.status}`)
      }
      setBusy(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setBusy(false)
    }
  }

  return (
    <Page>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
        <ShieldCheck size={18} style={{ color: 'var(--accent)' }} />
        <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>Sign in to AgentBoard</h1>
      </div>
      <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
        Paste your access token below.
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

      {!status.initialized && status.invite_url && (
        <div
          style={{
            marginBottom: '0.75rem',
            padding: '0.625rem 0.75rem',
            borderRadius: '0.375rem',
            background: 'color-mix(in srgb, var(--accent) 12%, transparent)',
            border: '1px solid color-mix(in srgb, var(--accent) 30%, transparent)',
            fontSize: '0.8125rem',
          }}
        >
          <div style={{ fontWeight: 600, marginBottom: 4 }}>First-time setup</div>
          This board hasn't been claimed yet. Open the invitation URL printed by
          the server to create the first admin:{' '}
          <a href={status.invite_url} style={{ color: 'var(--accent)' }}>
            open invite
          </a>
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
        Don't have a token? Ask an admin to send you an invite. Lost an
        existing one? Rotate on the host:{' '}
        <code style={{ background: 'var(--bg-secondary)', padding: '0.1rem 0.3rem', borderRadius: '0.25rem' }}>
          agentboard admin rotate &lt;user&gt; &lt;label&gt;
        </code>
      </p>
    </Page>
  )
}
