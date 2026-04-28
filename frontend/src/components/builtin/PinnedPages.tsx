import type { CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { Pin } from 'lucide-react'
import { useData } from '../../hooks/useData'

// <PinnedPages source="pinned" /> — operator-curated shortcut grid.
// The home page declares its pinned shortcuts in its own frontmatter:
//
//   ---
//   title: Home
//   pinned:
//     - { href: "/runbook/incident", title: "Incident runbook" }
//     - { href: "/principles", title: "Engineering principles" }
//   ---
//
// Each item is `{ href, title, summary? }`. Just static cards — no
// auto-aggregation, no "recently updated," no recency-as-signal at all.

interface PinnedItem {
  href?: string
  path?: string  // alias for href
  title: string
  summary?: string
}

interface PinnedPagesProps {
  source?: string
  /** Default contents when `source` is not provided in frontmatter. */
  fallback?: PinnedItem[]
}

const GRID: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(220px, 1fr))',
  gap: '0.75rem',
}
const CARD: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: '0.4rem',
  padding: '0.85rem 1rem',
  borderRadius: '0.625rem',
  border: '1px solid var(--border)',
  background: 'var(--bg)',
  textDecoration: 'none',
  color: 'var(--text)',
}
const TITLE: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: '0.4rem',
  fontWeight: 600,
}
const SUMMARY: CSSProperties = {
  fontSize: '0.8125rem',
  color: 'var(--text-secondary)',
  margin: 0,
}

export function PinnedPages({ source = 'pinned', fallback }: PinnedPagesProps) {
  const { data } = useData(source)
  const list = (Array.isArray(data) ? (data as PinnedItem[]) : null) ?? fallback ?? []

  if (list.length === 0) {
    return (
      <div style={{ color: 'var(--text-secondary)', fontSize: '0.875rem', padding: '0.5rem 0' }}>
        Nothing pinned. Add a `pinned: [{'{href, title}'}]` array to this page's frontmatter.
      </div>
    )
  }

  return (
    <div style={GRID}>
      {list.map((item, i) => {
        const to = item.href ?? item.path ?? '/'
        return (
          <Link key={`${to}-${i}`} to={to} style={CARD}>
            <div style={TITLE}>
              <Pin size={13} strokeWidth={2.5} style={{ color: 'var(--accent)' }} />
              {item.title}
            </div>
            {item.summary && <p style={SUMMARY}>{item.summary}</p>}
          </Link>
        )
      })}
    </div>
  )
}
