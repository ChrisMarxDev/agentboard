import { useEffect, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import Nav from './Nav'

const STORAGE_KEY = 'agentboard:nav-collapsed'

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const kiosk = new URLSearchParams(location.search).get('nochrome') === '1'

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return window.localStorage.getItem(STORAGE_KEY) === '1'
  })

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, collapsed ? '1' : '0')
  }, [collapsed])

  const showNav = !kiosk && !collapsed

  return (
    <div className="min-h-screen flex" style={{ background: 'var(--bg)' }}>
      {showNav && <Nav onCollapse={() => setCollapsed(true)} />}
      <main className="flex-1 p-8 max-w-5xl mx-auto w-full relative">
        {!kiosk && collapsed && (
          <button
            onClick={() => setCollapsed(false)}
            aria-label="Show navigation"
            title="Show navigation"
            className="fixed top-3 left-3 z-50 w-8 h-8 flex items-center justify-center rounded-md text-lg leading-none"
            style={{
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border)',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            ☰
          </button>
        )}
        {children}
      </main>
    </div>
  )
}
