import { useCallback, useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { Copy, Eye, EyeOff, KeyRound, Plus, RotateCw, ShieldCheck, Trash2 } from 'lucide-react'
import { useMe } from '../hooks/useMe'
import { getToken } from '../lib/session'
import {
  createTokenForUser,
  listTokensForUser,
  revokeToken,
  rotateToken,
  type CreatedToken,
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
        <div style={LABEL}>This browser&rsquo;s session token</div>
        <CurrentSessionCard />
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

// CurrentSessionCard surfaces the bearer token already in localStorage
// so a non-technical user can copy it into another tool (laptop env,
// MCP client, curl) without opening DevTools. Hidden by default — a
// shoulder-surf or screen-share shouldn't expose it. Anyone who already
// has this browser already has the token; we're only removing friction,
// not weakening the trust boundary.
function CurrentSessionCard() {
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)
  const tok = getToken()

  if (!tok) {
    return (
      <div style={{ ...CARD, color: 'var(--text-secondary)', fontSize: '0.8125rem' }}>
        No session token in this browser. (You shouldn&rsquo;t be seeing this page — sign in.)
      </div>
    )
  }

  function copy() {
    if (!tok) return
    void navigator.clipboard.writeText(tok).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    })
  }

  const masked = `${tok.slice(0, 6)}${'•'.repeat(Math.max(8, tok.length - 9))}${tok.slice(-3)}`

  return (
    <div style={CARD}>
      <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', marginBottom: '0.625rem' }}>
        Paste this into any tool that needs to talk to AgentBoard from another machine —
        a laptop env file, an MCP client config, a <code>curl -H &quot;Authorization: Bearer …&quot;</code> call.
        Treat it like a password. If it leaks, rotate the matching token below.
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
          {visible ? tok : masked}
        </code>
        <button
          style={BTN}
          onClick={() => setVisible((v) => !v)}
          title={visible ? 'Hide' : 'Show'}
        >
          {visible ? <EyeOff size={12} /> : <Eye size={12} />}
          {visible ? 'Hide' : 'Show'}
        </button>
        <button style={BTN} onClick={copy}>
          <Copy size={12} />
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
    </div>
  )
}

function RevealBanner({ token, onDismiss }: { token: CreatedToken; onDismiss: () => void }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    void navigator.clipboard.writeText(token.token).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    })
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
