import { useEffect, useState, type CSSProperties, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { ArrowRight, CheckCircle2, Copy, Mail } from 'lucide-react'
import {
  getInvitationPublic,
  redeemInvitation,
  type PublicInvitationView,
} from '../lib/auth'
import { copyToClipboard } from '../lib/clipboard'
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
  const [password, setPassword] = useState('')
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  // After redeem succeeds, the page becomes a "hand this to your
  // agent" briefing instead of bouncing straight to the dashboard.
  // Token is shown once for the agent-handoff prompt; it's also
  // visible at /tokens once the user lands on the dashboard. The
  // browser session is set via cookie by the redeem response, so
  // navigation to / works without any client-side token storage.
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
    if (password && password.length < 10) {
      setSubmitError('Password must be at least 10 characters.')
      return
    }
    setBusy(true)
    setSubmitError(null)
    try {
      const result = await redeemInvitation(id, clean, displayName.trim() || undefined, password || undefined)
      // The server set the agentboard_session + agentboard_csrf
      // cookies on the redeem response when a password was supplied,
      // so the SPA is already signed in. refreshMe primes the
      // useMe() hook for downstream components.
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
          <div>
            <div style={label}>Password (min 10 chars)</div>
            <input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={e => setPassword(e.target.value)}
              placeholder="••••••••"
              minLength={10}
              required
              style={input}
            />
            <div style={{ marginTop: '0.375rem', fontSize: '0.75rem', color: 'var(--text-secondary)' }}>
              Used to sign in to the dashboard at /login. Tokens for agents and CLI are
              issued automatically — see the next screen.
            </div>
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
// that the user drops into their agent.
//
// MCP is the primary path — most modern agents (Claude.ai, Cursor,
// Cody, Continue) call out through their host's network proxy with
// allowlists, so raw curl to a self-hosted IP often fails ("host
// blocked by network policy"). MCP-via-connector goes through the
// host's tool layer and just works once configured.
//
// The selector at the top picks the agent runtime; the prompt body
// updates accordingly. We focus on Claude Code (the CLI) and
// "Other / Generic MCP" — the latter covers Cursor/Cody/Continue
// and any future client that takes a URL + bearer header.
type AgentTarget = 'claude-code' | 'other'

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
  const [target, setTarget] = useState<AgentTarget>('claude-code')
  const base = window.location.origin

  // Two flavors:
  //
  // Claude Code — single self-bootstrapping prompt. The local agent
  // can shell out to `claude mcp add` itself, so we collapse setup,
  // permission pre-approval, and "first action" into one paste. The
  // `--scope user` flag plus the permissions.allow edit make
  // agentboard_* tools available everywhere without per-call prompts.
  // The prompt also tells the agent how to recover from the one
  // timing edge that can't be fixed mid-session: MCP servers register
  // at session start, so if the tools don't appear after running
  // `claude mcp add`, the agent surfaces "please restart Claude Code,
  // then re-paste this" and stops. The user re-pastes after restart
  // and the second run sees the server already registered → tools
  // available → proceeds.
  //
  // Other — keep the two-step shape because Cursor/Cody/Claude.ai
  // configure MCP via UI, not via a CLI an agent can run.
  const claudeCodePrompt = `You are now connected to AgentBoard, a persistent knowledge base for this project. Wire it up and use it.

Step 1 — register the MCP server and pre-approve its tools so you don't get prompted on every call. Run these in your shell (idempotent — safe to re-run):

    claude mcp add agentboard --scope user --transport http ${base}/mcp \\
      --header "Authorization: Bearer ${token}"

    # Skip the per-tool permission prompt for every agentboard_* call:
    mkdir -p ~/.claude && [ -f ~/.claude/settings.json ] || echo '{}' > ~/.claude/settings.json
    tmp=$(mktemp) && jq '.permissions.allow = ((.permissions.allow // []) + ["mcp__agentboard"] | unique)' ~/.claude/settings.json > "$tmp" && mv "$tmp" ~/.claude/settings.json

If \`claude mcp list\` already shows a "claude.ai Agentboard" connector marked "Needs authentication", remove it at https://claude.ai/settings/connectors first — it collides on the name and will shadow this bearer-token registration.

Then check whether the agentboard_* tools are available to you in this session:

  - If yes (you can see tools whose names start with "agentboard_"), continue with Step 2.
  - If no, the server is registered but Claude Code only loads MCP servers at session start. Tell me: "I've registered the AgentBoard MCP server. Please restart Claude Code (or run \`/clear\` then re-paste this prompt) so the agentboard_* tools load." Then stop and wait — do NOT try to bootstrap them by other means; raw HTTP calls from the sandbox won't work either.

Step 2 — once the tools are visible, your first call MUST be:

    agentboard_read({ paths: ["skills/agentboard/SKILL"] })

That returns the skill manifest in the unified-read shape (frontmatter + body + version). The body documents every surface (pages, files, data, components), the exact 10-tool MCP API, and the conventions you must follow. Read it once, then act.

Server:    ${base}
Username:  ${username} (role: ${role})

Never write directly to disk on the AgentBoard host — every change goes through MCP tools. The skill manifest covers all of it.

Now proceed with whatever I asked you to do, using AgentBoard as the persistent knowledge base for this project.`

  const otherSetupCommand = `URL:     ${base}/mcp
Header:  Authorization: Bearer ${token}`

  const otherSetupNotes = `Add it through your client's MCP connector UI (Cursor: Settings → MCP; Cody: settings.json mcpServers; Claude.ai: Settings → Connectors → Add custom). Then restart the session so the new tools register.`

  const otherAgentPrompt = `You are now connected to AgentBoard, a persistent knowledge base for this project.

Your first action MUST be to fetch the skill manifest — it documents every surface (pages, files, data, components), the exact 10-tool MCP API, and the conventions you must follow:

    agentboard_read({ paths: ["skills/agentboard/SKILL"] })

That returns the manifest in the unified-read shape (frontmatter + body + version). If the agentboard_* tools aren't available to you, the MCP server hasn't been wired up yet — stop and tell the human "I don't see the agentboard MCP tools; please add the MCP server and restart this session."

Server:    ${base}
Username:  ${username} (role: ${role})

Read the manifest once, then act. Never write directly to disk on the AgentBoard host — every change goes through MCP tools. The skill manifest covers all of it.

Now proceed with whatever the user asked you to do, using AgentBoard as the persistent knowledge base for this project.`

  const promptText = target === 'claude-code' ? claudeCodePrompt : otherAgentPrompt

  const copy = async () => {
    const ok = await copyToClipboard(promptText)
    if (ok) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }
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
          {target === 'claude-code' ? (
            <>
              One copy-paste. Drop it into your Claude Code session and
              the agent will register the MCP server, fetch the skill
              manifest, and take over. If a session restart is needed,
              the agent will tell you and stop cleanly.
            </>
          ) : (
            <>
              Two-step handoff: <b>you</b> wire AgentBoard as an MCP
              server in your client's connector UI, then paste the agent
              prompt into your chat.
            </>
          )}
        </p>

        <div style={{ display: 'flex', gap: '0.375rem', marginTop: '1.25rem', marginBottom: '1rem' }}>
          {(['claude-code', 'other'] as const).map((t) => {
            const active = target === t
            return (
              <button
                key={t}
                type="button"
                onClick={() => setTarget(t)}
                style={{
                  padding: '0.4rem 0.85rem',
                  borderRadius: '0.45rem',
                  fontSize: '0.8125rem',
                  fontWeight: 500,
                  cursor: 'pointer',
                  background: active ? 'var(--accent)' : 'transparent',
                  color: active ? 'white' : 'var(--text-secondary)',
                  border: `1px solid ${active ? 'var(--accent)' : 'var(--border)'}`,
                }}
              >
                {t === 'claude-code' ? 'Claude Code' : 'Other (Cursor, Cody, Claude.ai…)'}
              </button>
            )
          })}
        </div>

        {target === 'claude-code' ? (
          <StepBlock
            number={1}
            title="Paste this into Claude Code"
            body={promptText}
            note={
              <>
                Contains your token — treat like a password. If you lose
                it, rotate from <code>/tokens</code> on the dashboard.
              </>
            }
            onCopy={copy}
            copyLabel={copied ? 'Copied!' : 'Copy prompt'}
            monospace
          />
        ) : (
          <>
            <StepBlock
              number={1}
              title="Set up MCP (once, for you to do in your client's UI)"
              body={otherSetupCommand}
              note={otherSetupNotes}
              onCopy={async () => {
                const ok = await copyToClipboard(otherSetupCommand)
                if (ok) {
                  setCopied(true)
                  setTimeout(() => setCopied(false), 1500)
                }
              }}
              copyLabel={copied ? 'Copied!' : 'Copy URL + header'}
              monospace
            />
            <div style={{ height: '1.25rem' }} />
            <StepBlock
              number={2}
              title="Paste this to your agent (after restart)"
              body={promptText}
              note={
                <>
                  Contains your token — treat like a password. If you lose
                  it, rotate from <code>/tokens</code> on the dashboard.
                </>
              }
              onCopy={copy}
              copyLabel={copied ? 'Copied!' : 'Copy prompt'}
              monospace
            />
          </>
        )}

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


// StepBlock renders one of the two halves of the post-claim briefing —
// labeled with a step number so the human can see at a glance which
// part is theirs to run vs which to paste into the agent. Each block
// has its own copy button so the user does not accidentally copy the
// whole card.
function StepBlock({
  number,
  title,
  body,
  note,
  onCopy,
  copyLabel,
  monospace,
}: {
  number: number
  title: string
  body: string
  note: React.ReactNode
  onCopy: () => void
  copyLabel: string
  monospace?: boolean
}) {
  return (
    <div style={{
      border: "1px solid var(--border)",
      borderRadius: "0.6rem",
      padding: "0.875rem 1rem 1rem",
      background: "var(--bg)",
    }}>
      <div style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        marginBottom: "0.5rem",
      }}>
        <div style={{
          display: "flex",
          alignItems: "center",
          gap: "0.5rem",
        }}>
          <div style={{
            width: "1.5rem",
            height: "1.5rem",
            borderRadius: "9999px",
            background: "var(--accent)",
            color: "white",
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: "0.75rem",
            fontWeight: 600,
          }}>
            {number}
          </div>
          <div style={{ fontSize: "0.875rem", fontWeight: 500 }}>{title}</div>
        </div>
        <button
          type="button"
          onClick={onCopy}
          style={{
            display: "inline-flex",
            alignItems: "center",
            gap: "0.4rem",
            padding: "0.35rem 0.75rem",
            borderRadius: "0.4rem",
            background: "transparent",
            color: "var(--text-secondary)",
            border: "1px solid var(--border)",
            fontSize: "0.8125rem",
            cursor: "pointer",
          }}
        >
          <Copy size={12} />
          {copyLabel}
        </button>
      </div>
      <textarea
        readOnly
        value={body}
        onClick={(e) => (e.target as HTMLTextAreaElement).select()}
        style={{
          width: "100%",
          minHeight: monospace && body.split("\n").length > 4 ? "10rem" : "4.5rem",
          fontFamily: monospace ? "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" : "inherit",
          fontSize: "0.78rem",
          lineHeight: 1.5,
          padding: "0.75rem 0.875rem",
          border: "1px solid var(--border)",
          borderRadius: "0.45rem",
          background: "var(--bg-secondary)",
          color: "var(--text)",
          whiteSpace: "pre",
          overflow: "auto",
          resize: "vertical",
        }}
      />
      <div style={{
        fontSize: "0.78125rem",
        color: "var(--text-secondary)",
        marginTop: "0.5rem",
      }}>
        {note}
      </div>
    </div>
  )
}
