import { Link, useLocation } from 'react-router-dom'
import { ThemeSwitch } from './ThemeSwitch'
import Kbd from './Kbd'
import type { PageEntry } from '../../hooks/usePages'

interface NavProps {
  pages: PageEntry[]
  onCollapse?: () => void
  onOpenHelp?: () => void
}

export default function Nav({ pages, onCollapse, onOpenHelp }: NavProps) {
  const location = useLocation()

  return (
    <nav
      className="w-56 shrink-0 border-r p-4 flex flex-col gap-1 h-screen sticky top-0"
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
            className="h-7 flex items-center gap-1 rounded-md px-1.5 leading-none"
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
            <span className="text-sm">‹</span>
            <Kbd>B</Kbd>
          </button>
        )}
      </div>

      <div
        className="flex items-center justify-between px-3 pb-2 text-[10px] uppercase tracking-wide"
        style={{ color: 'var(--text-secondary)' }}
      >
        <span>Pages</span>
        <div className="flex items-center gap-1">
          <Kbd>J</Kbd>
          <Kbd>K</Kbd>
        </div>
      </div>

      <div className="flex-1 flex flex-col gap-1 overflow-y-auto">
        {pages.map((page, idx) => {
          const isActive = location.pathname === page.path
          const digit = idx < 9 ? String(idx + 1) : null
          return (
            <Link
              key={page.path}
              to={page.path}
              className="flex items-center justify-between gap-2 px-3 py-2 rounded-md text-sm transition-colors"
              style={{
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
              }}
            >
              <span className="truncate">{page.title}</span>
              {digit && <Kbd active={isActive}>{digit}</Kbd>}
            </Link>
          )
        })}
      </div>

      <div className="flex items-center gap-2 pt-2">
        <div className="flex-1">
          <ThemeSwitch />
        </div>
        {onOpenHelp && (
          <button
            onClick={onOpenHelp}
            aria-label="Show keyboard shortcuts"
            title="Keyboard shortcuts"
            className="h-8 flex items-center justify-center rounded-md px-2"
            style={{
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              cursor: 'pointer',
            }}
          >
            <Kbd>?</Kbd>
          </button>
        )}
      </div>
    </nav>
  )
}
