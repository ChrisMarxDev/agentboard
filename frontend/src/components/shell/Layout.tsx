import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import Nav from './Nav'
import Kbd from './Kbd'
import ShortcutsHelp from './ShortcutsHelp'
import { useKeyboardShortcuts, type ShortcutMap } from '../../hooks/useKeyboardShortcuts'
import { usePages } from '../../hooks/usePages'

const STORAGE_KEY = 'agentboard:nav-collapsed'

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const kiosk = new URLSearchParams(location.search).get('nochrome') === '1'

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return window.localStorage.getItem(STORAGE_KEY) === '1'
  })
  const [helpOpen, setHelpOpen] = useState(false)

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, collapsed ? '1' : '0')
  }, [collapsed])

  const pages = usePages()

  const shortcuts = useMemo<ShortcutMap>(() => {
    if (helpOpen) {
      return {
        Escape: () => setHelpOpen(false),
        '?': () => setHelpOpen(false),
      }
    }

    const map: ShortcutMap = {
      b: () => setCollapsed(c => !c),
      '?': () => setHelpOpen(true),
    }

    if (pages.length > 0) {
      const currentIdx = pages.findIndex(p => p.path === location.pathname)
      const wrap = (i: number) => (i + pages.length) % pages.length
      map.j = () => {
        const next = currentIdx < 0 ? 0 : wrap(currentIdx + 1)
        navigate(pages[next].path)
      }
      map.k = () => {
        const prev = currentIdx < 0 ? pages.length - 1 : wrap(currentIdx - 1)
        navigate(pages[prev].path)
      }
      for (let i = 0; i < Math.min(9, pages.length); i++) {
        const digit = String(i + 1)
        const target = pages[i].path
        map[digit] = () => navigate(target)
      }
    }

    return map
  }, [helpOpen, pages, location.pathname, navigate])

  useKeyboardShortcuts(shortcuts, !kiosk)

  const showNav = !kiosk && !collapsed

  return (
    <div className="min-h-screen flex" style={{ background: 'var(--bg)' }}>
      {showNav && (
        <Nav
          pages={pages}
          onCollapse={() => setCollapsed(true)}
          onOpenHelp={() => setHelpOpen(true)}
        />
      )}
      <main className="flex-1 p-8 max-w-5xl mx-auto w-full relative">
        {!kiosk && collapsed && (
          <button
            onClick={() => setCollapsed(false)}
            aria-label="Show navigation"
            title="Show navigation"
            className="fixed top-3 left-3 z-50 h-8 flex items-center gap-1.5 rounded-md px-2"
            style={{
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border)',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            <span className="text-lg leading-none">☰</span>
            <Kbd>B</Kbd>
          </button>
        )}
        {children}
      </main>
      <ShortcutsHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
    </div>
  )
}
