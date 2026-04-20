import type { ReactNode, CSSProperties } from 'react'

interface CardProps {
  children: ReactNode
  title?: string
  span?: number
}

export function Card({ children, title, span = 1 }: CardProps) {
  const style: CSSProperties = {
    background: 'var(--bg)',
    border: '1px solid var(--border)',
    borderRadius: '0.75rem',
    padding: '1.25rem 1.5rem',
    boxShadow: '0 1px 2px rgba(0, 0, 0, 0.04), 0 1px 3px rgba(0, 0, 0, 0.06)',
    gridColumn: span > 1 ? `span ${span}` : undefined,
    minWidth: 0,
  }

  return (
    <div className="ab-card" style={style}>
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
