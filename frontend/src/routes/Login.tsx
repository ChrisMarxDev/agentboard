import { useState, useEffect, type CSSProperties, type FormEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { ShieldCheck, Sparkles } from 'lucide-react'
import { apiFetch, claimBoard, fetchSetupStatus, setToken, type SessionUser } from '../lib/session'

// /login — the single auth surface.
//
// On mount we call /api/setup/status:
//   - initialized=true  → show the "paste token" form
//   - initialized=false → show the "claim this board" form
//
// The claim flow creates the first admin and hands back a token. Both
// forms end the same way: a token in localStorage and a redirect to
// wherever the user was going.

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

export default function Login() {
  const [search] = useSearchParams()
  const next = search.get('next') || '/'
  const reason = search.get('reason')

  // initialized: null = checking; true = show SignIn; false = show Claim.
  const [initialized, setInitialized] = useState<boolean | null>(null)

  useEffect(() => {
    document.title = 'Sign in — AgentBoard'
    fetchSetupStatus().then(setInitialized)
  }, [])

  if (initialized === null) {
    return (
      <Page>
        <div style={{ color: 'var(--text-secondary)' }}>Checking board state…</div>
      </Page>
    )
  }

  return initialized ? (
    <SignInForm next={next} reason={reason} />
  ) : (
    <ClaimForm next={next} />
  )
}

// -------- claim (first admin) --------

function ClaimForm({ next }: { next: string }) {
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [created, setCreated] = useState<{ username: string; token: string } | null>(null)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    const u = username.trim().toLowerCase()
    if (!u) return
    setBusy(true)
    setError(null)
    try {
      const result = await claimBoard(u, displayName.trim() || undefined)
      // Store + show the token once so the operator can copy it before
      // we redirect; the app remembers via localStorage regardless.
      setToken(result.token)
      setCreated(result)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setBusy(false)
    }
  }

  if (created) {
    return (
      <Page>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
          <ShieldCheck size={18} style={{ color: 'var(--success)' }} />
          <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>
            Welcome, @{created.username}
          </h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
          Your admin token is saved in this browser. Copy it somewhere safe
          — it's the only way back in from another browser, and it won't be
          shown again.
        </p>
        <code
          style={{
            display: 'block',
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
          {created.token}
        </code>
        <div style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
          <button
            style={{
              padding: '0.375rem 0.75rem',
              borderRadius: '0.375rem',
              border: '1px solid var(--border)',
              background: 'transparent',
              color: 'var(--text)',
              fontSize: '0.8125rem',
              cursor: 'pointer',
            }}
            onClick={() => void navigator.clipboard.writeText(created.token).catch(() => {})}
          >
            Copy token
          </button>
          <button style={BTN_PRIMARY} onClick={() => window.location.assign(next)}>
            Continue
          </button>
        </div>
      </Page>
    )
  }

  return (
    <Page>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
        <Sparkles size={18} style={{ color: 'var(--accent)' }} />
        <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>Claim this board</h1>
      </div>
      <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
        This AgentBoard instance hasn't been claimed yet. Pick a username —
        it'll be the first admin and what @mentions resolve to. You can add
        more users afterwards.
      </p>

      <form onSubmit={onSubmit}>
        <label style={{ ...LABEL, display: 'block', marginBottom: '0.375rem' }}>Username</label>
        <input
          autoFocus
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="alice"
          pattern="^[a-z][a-z0-9_-]{0,31}$"
          title="Lowercase letters, digits, _ or -; start with a letter; max 32"
          style={{ ...INPUT, fontFamily: 'ui-monospace, monospace' }}
          required
        />
        <label style={{ ...LABEL, display: 'block', marginTop: '0.75rem', marginBottom: '0.375rem' }}>
          Display name (optional)
        </label>
        <input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          placeholder="Alice Chen"
          style={INPUT}
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
          <button type="submit" disabled={busy || !username.trim()} style={BTN_PRIMARY}>
            {busy ? 'Claiming…' : 'Claim admin'}
          </button>
        </div>
      </form>

      <p style={{ marginTop: '1rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
        Automating a deploy? Mint the first admin on the host with{' '}
        <code style={{ background: 'var(--bg-secondary)', padding: '0.1rem 0.3rem', borderRadius: '0.25rem' }}>
          agentboard admin mint-admin &lt;name&gt;
        </code>{' '}
        instead.
      </p>
    </Page>
  )
}

// -------- sign in (existing admin/agent tokens) --------

function SignInForm({ next, reason }: { next: string; reason: string | null }) {
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
      const res = await apiFetch('/api/admin/me', {
        skipAuth: true,
        headers: { Authorization: `Bearer ${trimmed}` },
      })
      if (res.status === 200) {
        const me = (await res.json()) as SessionUser
        setToken(trimmed)
        void me
        window.location.assign(next)
        return
      }
      if (res.status === 403) {
        // Valid agent token — sign in; dashboard works.
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
    <Page>
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.75rem' }}>
        <ShieldCheck size={18} style={{ color: 'var(--accent)' }} />
        <h1 style={{ fontSize: '1.125rem', fontWeight: 600, margin: 0 }}>Sign in to AgentBoard</h1>
      </div>
      <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: '0 0 1rem' }}>
        Paste your access token. Any admin or agent token works.
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
        Lost the token? Run{' '}
        <code style={{ background: 'var(--bg-secondary)', padding: '0.1rem 0.3rem', borderRadius: '0.25rem' }}>
          agentboard admin mint-admin &lt;name&gt;
        </code>{' '}
        on the host to mint another admin.
      </p>
    </Page>
  )
}
