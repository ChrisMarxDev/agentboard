import { useEffect, useRef, useState } from 'react'
import { Check, ChevronUp, Copy, Magnet, Trash2, X } from 'lucide-react'
import { useGrab } from '../../hooks/useGrab'
import {
  clearPicks,
  copyToClipboard,
  materialize,
  removePick,
  setMode,
  type GrabFormat,
} from '../../lib/grab'

const FORMAT_LABEL: Record<GrabFormat, string> = {
  markdown: 'Markdown',
  xml: 'XML',
  json: 'JSON',
}

const FORMAT_HINT: Record<GrabFormat, string> = {
  markdown: 'Default — pastes cleanly anywhere',
  xml: 'Claude-style <context> tags',
  json: 'Structured for tool chaining',
}

export function GrabTray() {
  const { mode, picks } = useGrab()
  const [status, setStatus] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [format, setFormat] = useState<GrabFormat>('markdown')
  const [menuOpen, setMenuOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!menuOpen) return
    const onDown = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setMenuOpen(false)
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [menuOpen])

  if (!mode) return null

  const copy = async () => {
    setBusy(true)
    setStatus(null)
    try {
      if (picks.length === 0) {
        setStatus('Pick a card first')
        return
      }
      const text = await materialize(picks, format)
      await copyToClipboard(text)
      const words = text.trim().split(/\s+/).length
      setStatus(`Copied · ~${words} words`)
      setTimeout(() => setStatus(null), 2500)
    } catch (e) {
      setStatus(e instanceof Error ? e.message : 'copy failed')
    } finally {
      setBusy(false)
    }
  }

  const hasPicks = picks.length > 0

  return (
    <div
      role="region"
      aria-label="Grab bundle tray"
      style={{
        position: 'fixed',
        bottom: '1rem',
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 100,
        width: 'min(28rem, calc(100vw - 2rem))',
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        borderRadius: '0.875rem',
        boxShadow: '0 12px 32px rgba(0,0,0,0.14), 0 2px 6px rgba(0,0,0,0.08)',
        fontSize: '0.875rem',
      }}
    >
      {hasPicks && (
        <ul
          style={{
            listStyle: 'none',
            margin: 0,
            padding: '0.5rem 0.5rem 0.25rem',
            maxHeight: 'min(14rem, 40vh)',
            overflowY: 'auto',
            borderBottom: '1px solid var(--border)',
          }}
        >
          {picks.map(p => (
            <li
              key={`${p.page}#${p.cardId}`}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '0.5rem',
                padding: '0.25rem 0.5rem',
                borderRadius: '0.5rem',
              }}
            >
              <span
                style={{
                  flex: 1,
                  minWidth: 0,
                  display: 'flex',
                  flexDirection: 'column',
                  lineHeight: 1.25,
                }}
              >
                <span
                  style={{
                    color: 'var(--text)',
                    fontWeight: 500,
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {p.cardTitle || p.cardId}
                </span>
                <span
                  style={{
                    color: 'var(--text-secondary)',
                    fontSize: '0.7rem',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {p.page}
                </span>
              </span>
              <button
                type="button"
                onClick={() => removePick(p.page, p.cardId)}
                aria-label={`Remove ${p.cardTitle || p.cardId}`}
                title="Remove"
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  width: '1.5rem',
                  height: '1.5rem',
                  border: 'none',
                  background: 'transparent',
                  color: 'var(--text-secondary)',
                  cursor: 'pointer',
                  borderRadius: '0.375rem',
                }}
                onMouseEnter={e => {
                  e.currentTarget.style.background = 'var(--bg-secondary)'
                  e.currentTarget.style.color = 'var(--text)'
                }}
                onMouseLeave={e => {
                  e.currentTarget.style.background = 'transparent'
                  e.currentTarget.style.color = 'var(--text-secondary)'
                }}
              >
                <X size={14} strokeWidth={2} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: '0.5rem',
          padding: '0.5rem 0.625rem',
        }}
      >
        <Magnet size={16} style={{ color: 'var(--accent)', flexShrink: 0 }} />
        <span style={{ fontWeight: 600, color: 'var(--text)' }}>
          {picks.length} {picks.length === 1 ? 'pick' : 'picks'}
        </span>
        {status && (
          <span
            style={{
              color: 'var(--text-secondary)',
              fontSize: '0.75rem',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}
          >
            {status}
          </span>
        )}
        <div style={{ flex: 1 }} />

        <CopySplit
          format={format}
          busy={busy}
          disabled={!hasPicks}
          onCopy={copy}
          onOpenMenu={() => setMenuOpen(v => !v)}
          menuOpen={menuOpen}
        />

        {menuOpen && (
          <div
            ref={menuRef}
            role="menu"
            style={{
              position: 'absolute',
              right: '0.625rem',
              bottom: '100%',
              marginBottom: '0.5rem',
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              borderRadius: '0.625rem',
              boxShadow: '0 10px 24px rgba(0,0,0,0.14)',
              minWidth: '14rem',
              padding: '0.25rem',
              zIndex: 101,
            }}
          >
            {(['markdown', 'xml', 'json'] as GrabFormat[]).map(f => (
              <button
                key={f}
                type="button"
                role="menuitemradio"
                aria-checked={format === f}
                onClick={() => {
                  setFormat(f)
                  setMenuOpen(false)
                }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '0.5rem',
                  width: '100%',
                  padding: '0.5rem 0.625rem',
                  border: 'none',
                  background: 'transparent',
                  color: 'var(--text)',
                  textAlign: 'left',
                  cursor: 'pointer',
                  borderRadius: '0.375rem',
                  fontSize: '0.8125rem',
                }}
                onMouseEnter={e => {
                  e.currentTarget.style.background = 'var(--bg-secondary)'
                }}
                onMouseLeave={e => {
                  e.currentTarget.style.background = 'transparent'
                }}
              >
                <span
                  style={{
                    width: '1rem',
                    display: 'inline-flex',
                    justifyContent: 'center',
                  }}
                >
                  {format === f && <Check size={14} strokeWidth={2.25} style={{ color: 'var(--accent)' }} />}
                </span>
                <span style={{ display: 'flex', flexDirection: 'column', lineHeight: 1.2 }}>
                  <span style={{ fontWeight: 500 }}>{FORMAT_LABEL[f]}</span>
                  <span style={{ fontSize: '0.7rem', color: 'var(--text-secondary)' }}>
                    {FORMAT_HINT[f]}
                  </span>
                </span>
              </button>
            ))}
          </div>
        )}

        <IconBtn
          onClick={() => {
            clearPicks()
            setStatus(null)
          }}
          disabled={!hasPicks}
          label="Clear all picks"
        >
          <Trash2 size={14} />
        </IconBtn>
        <IconBtn onClick={() => setMode(false)} label="Leave grab mode">
          <X size={14} />
        </IconBtn>
      </div>
    </div>
  )
}

function CopySplit({
  format,
  busy,
  disabled,
  onCopy,
  onOpenMenu,
  menuOpen,
}: {
  format: GrabFormat
  busy: boolean
  disabled: boolean
  onCopy: () => void
  onOpenMenu: () => void
  menuOpen: boolean
}) {
  const base = {
    height: '1.875rem',
    fontSize: '0.8rem',
    fontWeight: 600,
    border: '1px solid transparent',
    cursor: disabled || busy ? 'not-allowed' : 'pointer',
    background: 'var(--accent)',
    color: 'white',
    opacity: disabled ? 0.55 : 1,
    display: 'inline-flex',
    alignItems: 'center',
    gap: '0.35rem',
  } as const
  return (
    <div style={{ display: 'inline-flex' }}>
      <button
        type="button"
        onClick={onCopy}
        disabled={disabled || busy}
        title={`Copy as ${FORMAT_LABEL[format]}`}
        style={{
          ...base,
          padding: '0 0.75rem',
          borderTopLeftRadius: '9999px',
          borderBottomLeftRadius: '9999px',
        }}
      >
        <Copy size={13} strokeWidth={2.25} />
        Copy {FORMAT_LABEL[format]}
      </button>
      <button
        type="button"
        onClick={onOpenMenu}
        disabled={busy}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label="Choose copy format"
        title="Choose format"
        style={{
          ...base,
          padding: '0 0.4rem',
          borderTopRightRadius: '9999px',
          borderBottomRightRadius: '9999px',
          borderLeft: '1px solid rgba(255,255,255,0.28)',
        }}
      >
        <ChevronUp size={14} strokeWidth={2.25} />
      </button>
    </div>
  )
}

function IconBtn({
  children,
  onClick,
  disabled,
  label,
}: {
  children: React.ReactNode
  onClick: () => void
  disabled?: boolean
  label: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: '1.875rem',
        height: '1.875rem',
        border: '1px solid var(--border)',
        background: 'transparent',
        color: 'var(--text-secondary)',
        borderRadius: '9999px',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1,
      }}
      onMouseEnter={e => {
        if (disabled) return
        e.currentTarget.style.background = 'var(--bg-secondary)'
        e.currentTarget.style.color = 'var(--text)'
      }}
      onMouseLeave={e => {
        e.currentTarget.style.background = 'transparent'
        e.currentTarget.style.color = 'var(--text-secondary)'
      }}
    >
      {children}
    </button>
  )
}
