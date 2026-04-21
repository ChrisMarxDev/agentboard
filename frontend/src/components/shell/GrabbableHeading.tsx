import { useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'
import { useGrab } from '../../hooks/useGrab'
import { isHeadingPicked, slug, togglePick } from '../../lib/grab'

interface GrabbableHeadingProps {
  level: 1 | 2 | 3
  children?: ReactNode
}

function flattenText(node: ReactNode): string {
  if (node == null || typeof node === 'boolean') return ''
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(flattenText).join('')
  if (typeof node === 'object' && 'props' in (node as object)) {
    return flattenText((node as { props?: { children?: ReactNode } }).props?.children)
  }
  return ''
}

/**
 * Replaces the default h1/h2/h3 tags in MDX output. In grab mode each heading
 * becomes a pick target — hover shows a checkbox, click toggles the pick.
 * A "section" materializes on the server as from this heading to the next at
 * equal-or-higher level.
 */
export function GrabbableHeading({ level, children }: GrabbableHeadingProps) {
  const { mode } = useGrab()
  const location = useLocation()
  const page = location.pathname || '/'
  const text = flattenText(children).trim()
  const headingSlug = slug(text)
  const grabbable = mode && Boolean(headingSlug)
  const picked = grabbable && isHeadingPicked(page, headingSlug)

  const style = {
    position: 'relative' as const,
    // A thin accent band on the left when picked — the content of a section
    // isn't a box, so this is the clearest "this is selected" cue.
    borderLeft: picked ? '3px solid var(--accent)' : '3px solid transparent',
    paddingLeft: grabbable ? '0.75rem' : undefined,
    marginLeft: grabbable ? '-0.75rem' : undefined,
    cursor: grabbable ? 'pointer' : undefined,
    transition: 'border-color 0.15s',
  }

  const toggle = (e: React.MouseEvent) => {
    if (!grabbable) return
    const target = e.target as HTMLElement
    if (target.closest('a, button, input')) return
    e.preventDefault()
    togglePick({
      kind: 'heading',
      page,
      headingSlug,
      headingText: text,
      headingLevel: level,
    })
  }

  const commonProps = {
    id: headingSlug || undefined,
    'data-grab-heading': headingSlug || undefined,
    'data-grab-level': level,
    style,
    onClick: grabbable ? toggle : undefined,
  } as const

  const checkbox = grabbable ? (
    <GrabCheckbox
      picked={!!picked}
      onToggle={() => togglePick({
        kind: 'heading',
        page,
        headingSlug,
        headingText: text,
        headingLevel: level,
      })}
    />
  ) : null

  if (level === 1) return <h1 {...commonProps}>{checkbox}{children}</h1>
  if (level === 2) return <h2 {...commonProps}>{checkbox}{children}</h2>
  return <h3 {...commonProps}>{checkbox}{children}</h3>
}

function GrabCheckbox({ picked, onToggle }: { picked: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-label={picked ? 'Remove section from grab bundle' : 'Add section to grab bundle'}
      aria-pressed={picked}
      onClick={e => {
        e.stopPropagation()
        onToggle()
      }}
      style={{
        position: 'absolute',
        top: '50%',
        left: '-1.75rem',
        transform: 'translateY(-50%)',
        width: '20px',
        height: '20px',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: '4px',
        border: `1px solid ${picked ? 'var(--accent)' : 'var(--border)'}`,
        background: picked ? 'var(--accent)' : 'var(--bg)',
        color: picked ? 'white' : 'var(--text-secondary)',
        cursor: 'pointer',
        fontSize: '11px',
        lineHeight: 1,
        padding: 0,
        zIndex: 2,
      }}
    >
      {picked ? '✓' : ''}
    </button>
  )
}
