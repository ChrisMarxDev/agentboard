import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

interface PageEntry {
  path: string
  title: string
  order: number
}

interface NavProps {
  onCollapse?: () => void
}

export default function Nav({ onCollapse }: NavProps) {
  const [pages, setPages] = useState<PageEntry[]>([])
  const location = useLocation()

  useEffect(() => {
    const load = () => fetch('/api/pages').then(r => r.json()).then(setPages).catch(() => {})
    load()
    window.addEventListener('agentboard:page-updated', load)
    return () => window.removeEventListener('agentboard:page-updated', load)
  }, [])

  return (
    <nav
      className="w-56 shrink-0 border-r p-4 flex flex-col gap-1"
      style={{ borderColor: 'var(--border)', background: 'var(--bg-secondary)' }}
    >
      <div className="flex items-center justify-between mb-4">
        <div className="font-semibold text-lg" style={{ color: 'var(--text)' }}>
          AgentBoard
        </div>
        {onCollapse && (
          <button
            onClick={onCollapse}
            aria-label="Hide navigation"
            title="Hide navigation"
            className="w-7 h-7 flex items-center justify-center rounded-md text-sm leading-none"
            style={{
              background: 'transparent',
              border: '1px solid transparent',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
            onMouseEnter={e => {
              e.currentTarget.style.background = 'var(--bg)'
              e.currentTarget.style.borderColor = 'var(--border)'
            }}
            onMouseLeave={e => {
              e.currentTarget.style.background = 'transparent'
              e.currentTarget.style.borderColor = 'transparent'
            }}
          >
            ‹
          </button>
        )}
      </div>
      {pages.map(page => {
        const isActive = location.pathname === page.path
        return (
          <Link
            key={page.path}
            to={page.path}
            className="block px-3 py-2 rounded-md text-sm transition-colors"
            style={{
              background: isActive ? 'var(--accent-light)' : 'transparent',
              color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
            }}
          >
            {page.title}
          </Link>
        )
      })}
    </nav>
  )
}
