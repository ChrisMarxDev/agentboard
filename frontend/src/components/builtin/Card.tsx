import type { ReactNode, CSSProperties } from 'react'
import { useGrab } from '../../hooks/useGrab'
import { isPicked, slug, togglePick } from '../../lib/grab'

interface CardProps {
  children: ReactNode
  title?: string
  span?: number
}

export function Card({ children, title, span = 1 }: CardProps) {
  const { mode } = useGrab()
  // Re-subscribing via useGrab ensures we re-render when the picks list
  // changes so the checkmark state reflects the live truth.
  const page = typeof window !== 'undefined' ? window.location.pathname : '/'
  const cardId = title ? slug(title) : ''
  const grabbable = mode && Boolean(cardId)
  const picked = grabbable && isPicked(page, cardId)

  const style: CSSProperties = {
    background: 'var(--bg)',
    border: `1px solid ${picked ? 'var(--accent)' : 'var(--border)'}`,
    borderRadius: '0.75rem',
    padding: '1.25rem 1.5rem',
    boxShadow: picked
      ? '0 0 0 2px var(--accent-light), 0 1px 3px rgba(0,0,0,0.06)'
      : '0 1px 2px rgba(0, 0, 0, 0.04), 0 1px 3px rgba(0, 0, 0, 0.06)',
    gridColumn: span > 1 ? `span ${span}` : undefined,
    minWidth: 0,
    position: 'relative',
    cursor: grabbable ? 'pointer' : undefined,
    transition: 'border-color 0.15s, box-shadow 0.15s',
  }

  const handleToggle = (e: React.MouseEvent) => {
    if (!grabbable) return
    // Don't grab when the user clicks a link/button inside the card.
    const target = e.target as HTMLElement
    if (target.closest('a, button, input, select, textarea')) return
    e.preventDefault()
    togglePick({ page, cardId, cardTitle: title })
  }

  return (
    <div
      className="ab-card"
      style={style}
      id={cardId || undefined}
      data-card-id={cardId || undefined}
      data-card-title={title || undefined}
      onClick={grabbable ? handleToggle : undefined}
    >
      {grabbable && (
        <GrabCheckbox
          picked={!!picked}
          onToggle={() => togglePick({ page, cardId, cardTitle: title })}
        />
      )}
      {title && (
        <div
          className="text-xs font-semibold uppercase tracking-wide mb-2"
          style={{ color: 'var(--text-secondary)' }}
        >
          {title}
        </div>
      )}
      <div className="card-body">{children}</div>
    </div>
  )
}

function GrabCheckbox({ picked, onToggle }: { picked: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-label={picked ? 'Remove from grab bundle' : 'Add to grab bundle'}
      aria-pressed={picked}
      onClick={e => {
        e.stopPropagation()
        onToggle()
      }}
      style={{
        position: 'absolute',
        top: '0.5rem',
        right: '0.5rem',
        width: '22px',
        height: '22px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: '6px',
        border: `1px solid ${picked ? 'var(--accent)' : 'var(--border)'}`,
        background: picked ? 'var(--accent)' : 'var(--bg)',
        color: picked ? 'white' : 'var(--text-secondary)',
        cursor: 'pointer',
        fontSize: '12px',
        lineHeight: 1,
        padding: 0,
        zIndex: 2,
      }}
    >
      {picked ? '✓' : ''}
    </button>
  )
}
