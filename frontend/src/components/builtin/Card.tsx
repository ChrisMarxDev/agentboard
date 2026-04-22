import type { ReactNode, CSSProperties } from 'react'
import { useNavigate } from 'react-router-dom'
import { useGrab } from '../../hooks/useGrab'
import { isCardPicked, slug, togglePick } from '../../lib/grab'

interface CardProps {
  children: ReactNode
  title?: string
  span?: number
  /**
   * Optional href. When set, the whole card becomes a clickable navigation
   * target (internal SPA route if the href starts with `/`, external link
   * otherwise — opens in a new tab). Inner interactive elements (links,
   * buttons, inputs) are still clickable and short-circuit the card-level
   * navigation so nested Grab / form / submenu controls keep working.
   */
  href?: string
}

export function Card({ children, title, span = 1, href }: CardProps) {
  const navigate = useNavigate()
  const { mode } = useGrab()
  // Re-subscribing via useGrab ensures we re-render when the picks list
  // changes so the checkmark state reflects the live truth.
  const page = typeof window !== 'undefined' ? window.location.pathname : '/'
  const cardId = title ? slug(title) : ''
  const grabbable = mode && Boolean(cardId)
  const picked = grabbable && isCardPicked(page, cardId)
  const linkable = Boolean(href) && !grabbable
  const external = href ? /^https?:\/\//.test(href) : false

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
    cursor: grabbable || linkable ? 'pointer' : undefined,
    transition: 'border-color 0.15s, box-shadow 0.15s, transform 0.15s',
  }

  const isInteractiveTarget = (el: HTMLElement | null): boolean =>
    Boolean(el?.closest('a, button, input, select, textarea, [role="button"]'))

  const handleClick = (e: React.MouseEvent) => {
    const target = e.target as HTMLElement
    if (isInteractiveTarget(target)) return // let the inner control handle it

    if (grabbable) {
      e.preventDefault()
      togglePick({ kind: 'card', page, cardId, cardTitle: title })
      return
    }

    if (linkable && href) {
      // Support cmd/ctrl/middle-click for new tab.
      if (e.metaKey || e.ctrlKey || e.button === 1) return
      e.preventDefault()
      if (external) {
        window.open(href, '_blank', 'noopener,noreferrer')
      } else {
        navigate(href)
      }
    }
  }

  return (
    <div
      className="ab-card"
      style={style}
      id={cardId || undefined}
      data-card-id={cardId || undefined}
      data-card-title={title || undefined}
      data-href={href || undefined}
      role={linkable ? 'link' : undefined}
      tabIndex={linkable ? 0 : undefined}
      onClick={grabbable || linkable ? handleClick : undefined}
      onKeyDown={
        linkable
          ? e => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault()
                if (href && external) {
                  window.open(href, '_blank', 'noopener,noreferrer')
                } else if (href) {
                  navigate(href)
                }
              }
            }
          : undefined
      }
      onMouseEnter={
        linkable
          ? e => {
              e.currentTarget.style.borderColor = 'var(--accent)'
              e.currentTarget.style.boxShadow =
                '0 0 0 1px var(--accent-light), 0 1px 3px rgba(0,0,0,0.08)'
            }
          : undefined
      }
      onMouseLeave={
        linkable
          ? e => {
              e.currentTarget.style.borderColor = picked ? 'var(--accent)' : 'var(--border)'
              e.currentTarget.style.boxShadow = picked
                ? '0 0 0 2px var(--accent-light), 0 1px 3px rgba(0,0,0,0.06)'
                : '0 1px 2px rgba(0, 0, 0, 0.04), 0 1px 3px rgba(0, 0, 0, 0.06)'
            }
          : undefined
      }
    >
      {grabbable && (
        <GrabCheckbox
          picked={!!picked}
          onToggle={() => togglePick({ kind: 'card', page, cardId, cardTitle: title })}
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
