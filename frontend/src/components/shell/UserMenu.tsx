import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ChevronDown, KeyRound, ShieldCheck, LogOut } from 'lucide-react'
import { useMe } from '../../hooks/useMe'
import { redirectToLogin, signOut } from '../../lib/session'

// UserMenu — persistent "signed in as" indicator anchored top-right of
// the shell. Renders a pill with the user's avatar color + name, opens
// a dropdown with tokens, admin (if applicable), and sign-out.
//
// Hidden when useMe() returns null (unauthenticated / loading). Hidden
// on public-route visits where the shell doesn't render anyway.

export function UserMenu() {
  const me = useMe()
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLButtonElement>(null)
  const popover = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    if (!open) return
    const off = (e: MouseEvent) => {
      if (
        anchor.current?.contains(e.target as Node) ||
        popover.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', off)
    return () => document.removeEventListener('mousedown', off)
  }, [open])

  if (!me) return null

  const triggerStyle: CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: '0.4rem',
    padding: '0.3rem 0.6rem',
    borderRadius: '9999px',
    border: '1px solid var(--border)',
    background: 'var(--bg)',
    cursor: 'pointer',
    color: 'var(--text)',
    fontSize: '0.8125rem',
    fontWeight: 500,
  }
  const dot: CSSProperties = {
    width: '0.55rem',
    height: '0.55rem',
    borderRadius: '9999px',
    background: me.avatar_color ?? 'var(--accent)',
    flexShrink: 0,
  }
  const wrap: CSSProperties = {
    position: 'fixed',
    top: 12,
    right: 12,
    zIndex: 40,
  }
  const pop: CSSProperties = {
    position: 'absolute',
    top: '100%',
    right: 0,
    marginTop: 6,
    minWidth: 200,
    background: 'var(--bg)',
    border: '1px solid var(--border)',
    borderRadius: '0.5rem',
    boxShadow: '0 4px 12px rgba(0,0,0,0.08)',
    padding: '0.25rem',
  }
  const item: CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: '0.5rem',
    padding: '0.5rem 0.625rem',
    borderRadius: '0.375rem',
    color: 'var(--text)',
    fontSize: '0.875rem',
    cursor: 'pointer',
    background: 'transparent',
    border: 'none',
    width: '100%',
    textDecoration: 'none',
    textAlign: 'left',
  }
  const roleBadge: CSSProperties = {
    display: 'inline-block',
    padding: '0.05rem 0.4rem',
    borderRadius: '9999px',
    background: 'var(--bg-secondary)',
    fontSize: '0.6875rem',
    letterSpacing: '0.02em',
    textTransform: 'uppercase',
    color: 'var(--text-secondary)',
    marginLeft: 'auto',
  }

  const label = me.display_name || `@${me.username}`

  return (
    <div style={wrap}>
      <button
        ref={anchor}
        onClick={() => setOpen(v => !v)}
        style={triggerStyle}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <span aria-hidden style={dot} />
        {label}
        <ChevronDown size={12} />
      </button>
      {open && (
        <div ref={popover} style={pop} role="menu">
          <div
            style={{
              padding: '0.5rem 0.625rem 0.625rem',
              borderBottom: '1px solid var(--border)',
              fontSize: '0.8125rem',
              color: 'var(--text-secondary)',
            }}
          >
            <div style={{ color: 'var(--text)', fontWeight: 600 }}>@{me.username}</div>
            <div style={{ marginTop: 2 }}>
              {me.display_name && <span>{me.display_name} · </span>}
              <span style={roleBadge}>{me.kind}</span>
            </div>
          </div>
          <button
            style={item}
            onClick={() => {
              setOpen(false)
              navigate('/tokens')
            }}
          >
            <KeyRound size={14} />
            My tokens
          </button>
          {me.kind === 'admin' && (
            <Link
              to="/admin"
              style={item}
              onClick={() => setOpen(false)}
            >
              <ShieldCheck size={14} />
              Admin
            </Link>
          )}
          <div style={{ borderTop: '1px solid var(--border)', margin: '0.25rem 0' }} />
          <button
            style={{ ...item, color: 'var(--error)' }}
            onClick={async () => {
              await signOut()
              redirectToLogin()
            }}
          >
            <LogOut size={14} />
            Sign out
          </button>
        </div>
      )}
    </div>
  )
}
