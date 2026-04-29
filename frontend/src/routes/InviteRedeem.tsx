import { useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowRight, CheckCircle2, Copy, Mail } from 'lucide-react'
import {
  getInvitationPublic,
  redeemInvitation,
  type PublicInvitationView,
} from '../lib/auth'
import { setToken } from '../lib/session'
import { refreshMe } from '../hooks/useMe'

// Public redeem page at /invite/:id. Anonymous — no token required to
// view. Calls GET /api/invitations/{id} to show the inviter's name +
// role, then POSTs the redeem. On success stores the returned token
// and redirects to /.
//
// Failure modes:
//   - 404: invite unusable (expired / redeemed / revoked / never existed)
//   - 409: username taken — show inline error, invite is still valid
//   - 400: invalid username — validate client-side too
//   - 410: invite consumed between loading the page and submitting
//   - 500: server error

export default function InviteRedeem() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [invite, setInvite] = useState<PublicInvitationView | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [username, setUsername] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // After redeem succeeds, the page becomes a "hand this to your
  // agent" briefing instead of bouncing straight to the dashboard.
  // Token is held in component state — already saved to localStorage
  // and visible at /tokens once the user lands on the dashboard,
  // but this is the one moment we can pre-bake a copyable prompt.
  const [claimedToken, setClaimedToken] = useState<string | null>(null)

  useEffect(() => {
    getInvitationPublic(id)
      .then(setInvite)
      .catch((e: Error) => setLoadError(e.message))
  }, [id])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const clean = username.trim().toLowerCase()
    if (!/^[a-z][a-z0-9_-]{0,31}$/.test(clean)) {
      setSubmitError('Username must start with a letter and contain only a-z, 0-9, _ or - (max 32 chars)')
      return
    }
    setBusy(true)
    setSubmitError(null)
    try {
      const result = await redeemInvitation(id, clean, displayName.trim() || undefined)
      setToken(result.token)
      refreshMe()
      // Show the agent-handoff briefing instead of navigating. The
      // user clicks "Continue" once they've copied the prompt.
      setClaimedToken(result.token)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const shell: CSSProperties = {
    minHeight: '100dvh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '2rem',
    background: 'var(--bg-secondary)',
    color: 'var(--text)',
  }
  const card: CSSProperties = {
    width: '100%',
    maxWidth: '28rem',
    background: 'var(--bg)',
    border: '1px solid var(--border)',
    borderRadius: '0.75rem',
    padding: '2rem 2rem 1.75rem',
    boxShadow: '0 10px 30px rgba(0,0,0,0.08)',
  }
  const input: CSSProperties = {
    width: '100%',
    padding: '0.625rem 0.875rem',
    border: '1px solid var(--border)',
    borderRadius: '0.5rem',
    background: 'var(--bg)',
    color: 'var(--text)',
    fontSize: '0.9375rem',
    outline: 'none',
  }
  const btn: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: '0.5rem',
    width: '100%',
    padding: '0.625rem 1rem',
    borderRadius: '0.5rem',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    fontSize: '0.9375rem',
    fontWeight: 500,
    cursor: 'pointer',
  }
  const label: CSSProperties = {
    fontSize: '0.75rem',
    fontWeight: 600,
    letterSpacing: '0.04em',
    textTransform: 'uppercase',
    color: 'var(--text-secondary)',
    marginBottom: '0.375rem',
  }
  const role: CSSProperties = {
    display: 'inline-block',
    padding: '0.125rem 0.5rem',
    borderRadius: '9999px',
    background: 'color-mix(in srgb, var(--accent) 12%, transparent)',
    color: 'var(--text)',
    fontSize: '0.75rem',
    textTransform: 'uppercase',
    letterSpacing: '0.04em',
    fontWeight: 600,
  }

  if (loadError) {
    return (
      <div style={shell}>
        <div style={card}>
          <h1 style={{ fontSize: '1.125rem', margin: '0 0 0.75rem' }}>
            Invitation unavailable
          </h1>
          <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', margin: 0 }}>
            This invitation link is either expired, already used, or never existed.
            Ask the admin for a fresh invite.
          </p>
        </div>
      </div>
    )
  }

  if (!invite) {
    return (
      <div style={shell}>
        <div style={card}>
          <div style={{ color: 'var(--text-secondary)' }}>Loading invitation…</div>
        </div>
      </div>
    )
  }

  if (claimedToken) {
    return (
      <ClaimedBriefing
        token={claimedToken}
        username={username}
        role={invite.role}
        onContinue={() => navigate('/')}
      />
    )
  }

  return (
    <div style={shell}>
      <form onSubmit={submit} style={card}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.625rem', marginBottom: '0.75rem' }}>
          <Mail size={20} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.125rem', margin: 0 }}>
            {invite.bootstrap ? 'Claim this board' : 'You\'re invited'}
          </h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', marginTop: 0 }}>
          {invite.bootstrap ? (
            <>
              You're about to become the first admin of this AgentBoard instance.
              Pick a username — you can't change it later.
            </>
          ) : (
            <>
              <b>@{invite.created_by}</b> invited you to join this AgentBoard
              as a{' '}
              <span style={role}>{invite.role}</span>
              {invite.label && <> · <i>{invite.label}</i></>}.
              Pick a username — you can't change it later.
            </>
          )}
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem', marginTop: '1.25rem' }}>
          <div>
            <div style={label}>Username</div>
            <input
              autoFocus
              value={username}
              onChange={e => setUsername(e.target.value)}
              placeholder="dana"
              style={input}
              maxLength={32}
              required
              pattern="[a-z][a-z0-9_\-]*"
            />
          </div>
          <div>
            <div style={label}>Display name (optional)</div>
            <input
              value={displayName}
              onChange={e => setDisplayName(e.target.value)}
              placeholder="Dana Kim"
              style={input}
            />
          </div>
        </div>

        {submitError && (
          <div
            style={{
              marginTop: '0.875rem',
              padding: '0.5rem 0.75rem',
              background: 'color-mix(in srgb, var(--error) 12%, transparent)',
              color: 'var(--error)',
              borderRadius: '0.375rem',
              fontSize: '0.8125rem',
            }}
          >
            {submitError}
          </div>
        )}

        <button type="submit" style={btn} disabled={busy || !username} aria-busy={busy}>
          <CheckCircle2 size={14} />
          {busy ? 'Claiming…' : 'Claim account'}
        </button>
      </form>
    </div>
  )
}

// ClaimedBriefing renders the post-redeem agent-handoff. The pitch:
// the human just claimed an account; their actual job is to put an
// AI agent in front of AgentBoard. We pre-bake a copy-paste prompt
// that includes the token, server URL, and exact endpoints the agent
// should hit first. The user pastes that into Claude / Cursor /
// Cody / whatever and the agent takes over.
function ClaimedBriefing({
  token,
  username,
  role,
  onContinue,
}: {
  token: string
  username: string
  role: string
  onContinue: () => void
}) {
  const [copied, setCopied] = useState(false)
  const base = window.location.origin

  // The text below is intentionally addressed in second person to
  // the AGENT, not to the human. The human's job is to copy + paste;
  // the agent reads the prompt and acts on it.
  const promptText = `You are now using AgentBoard as the persistent knowledge base for this project. AgentBoard is a content surface for agent teams: agents write — pages, files, tasks, data — and humans read.

Server:        ${base}
Auth token:    ${token}
Username:      ${username} (role: ${role})

Treat the token like a password. Pass it on every request:

    Authorization: Bearer ${token}

Before doing anything else, fetch the skill manifest. It documents every surface (pages, files, tasks, data, components), the exact API shape for writes, and the conventions agents are expected to follow:

    curl -H "Authorization: Bearer ${token}" \\
      ${base}/api/skills/agentboard

That single document is the contract. Read it once, then act.

Quick health checks:

    curl -H "Authorization: Bearer ${token}" ${base}/api/health
    curl -H "Authorization: Bearer ${token}" ${base}/api/me

If your runtime supports MCP, AgentBoard exposes a tool server too. From Claude Code:

    claude mcp add agentboard --transport http ${base}/mcp \\
      --header "Authorization: Bearer ${token}"

Never write directly to disk on the AgentBoard host — every change must go through REST or MCP. The skill manifest covers all of it.

Now proceed with whatever the user asked you to do, using AgentBoard as the persistent knowledge base for this project.`

  const copy = () => {
    void navigator.clipboard.writeText(promptText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const wideShell: CSSProperties = {
    minHeight: '100dvh',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    padding: '2rem',
    background: 'var(--bg-secondary)',
    color: 'var(--text)',
  }
  const wideCard: CSSProperties = {
    width: '100%',
    maxWidth: '44rem',
    background: 'var(--bg)',
    border: '1px solid var(--border)',
    borderRadius: '0.75rem',
    padding: '2rem 2rem 1.75rem',
    boxShadow: '0 10px 30px rgba(0,0,0,0.08)',
  }
  const promptArea: CSSProperties = {
    width: '100%',
    minHeight: '14rem',
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    fontSize: '0.78rem',
    lineHeight: 1.5,
    padding: '0.875rem 1rem',
    border: '1px solid var(--border)',
    borderRadius: '0.5rem',
    background: 'var(--bg-secondary)',
    color: 'var(--text)',
    whiteSpace: 'pre',
    overflow: 'auto',
    resize: 'vertical',
  }
  const primaryBtn: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    gap: '0.5rem',
    padding: '0.55rem 1rem',
    borderRadius: '0.5rem',
    background: 'var(--accent)',
    color: 'white',
    border: 'none',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
  }
  const ghostBtn: CSSProperties = {
    ...primaryBtn,
    background: 'transparent',
    color: 'var(--text-secondary)',
    border: '1px solid var(--border)',
  }

  return (
    <div style={wideShell}>
      <div style={wideCard}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.625rem', marginBottom: '0.5rem' }}>
          <CheckCircle2 size={20} style={{ color: 'var(--accent)' }} />
          <h1 style={{ fontSize: '1.125rem', margin: 0 }}>
            You're in, @{username}
          </h1>
        </div>
        <p style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', marginTop: 0 }}>
          Your account is created and a token is saved in this browser.
          The fastest way to get going is to hand the briefing below to
          your AI agent — it tells the agent how to authenticate, where
          to find the AgentBoard skill, and how to start writing.
        </p>

        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '1.25rem', marginBottom: '0.5rem' }}>
          <div style={{
            fontSize: '0.6875rem',
            fontWeight: 600,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
            color: 'var(--text-secondary)',
          }}>
            Briefing for your agent
          </div>
          <button type="button" onClick={copy} style={primaryBtn}>
            <Copy size={14} />
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>

        <textarea
          readOnly
          value={promptText}
          onClick={(e) => (e.target as HTMLTextAreaElement).select()}
          style={promptArea}
        />

        <p style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', marginTop: '0.875rem' }}>
          Paste this into your agent's chat. It contains your token —
          treat the message like a password. If you lose it, you can
          rotate the token from <code>/tokens</code> on the dashboard.
        </p>

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.5rem', marginTop: '1.25rem' }}>
          <button type="button" onClick={onContinue} style={ghostBtn}>
            Continue to dashboard
            <ArrowRight size={14} />
          </button>
        </div>
      </div>
    </div>
  )
}
