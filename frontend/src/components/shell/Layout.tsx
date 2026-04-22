import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import Nav, { NAV_DEFAULT_WIDTH, NAV_MAX_WIDTH, NAV_MIN_WIDTH } from './Nav'
import Kbd from './Kbd'
import ShortcutsHelp from './ShortcutsHelp'
import { useKeyboardShortcuts, type ShortcutMap } from '../../hooks/useKeyboardShortcuts'
import { usePages } from '../../hooks/usePages'
import { useFiles } from '../../hooks/useFiles'
import { buildContentTree, flattenContentTreePageHrefs } from '../../lib/contentTree'
import { GrabTray } from './GrabTray'
import CopyToast from './CopyToast'
import { copyPageSource, pagePathFromLocation } from '../../lib/copyPage'

const STORAGE_KEY = 'agentboard:nav-collapsed'
const WIDTH_STORAGE_KEY = 'agentboard:nav-width'

function clampWidth(w: number): number {
  if (!Number.isFinite(w)) return NAV_DEFAULT_WIDTH
  return Math.max(NAV_MIN_WIDTH, Math.min(NAV_MAX_WIDTH, w))
}

function loadWidth(): number {
  if (typeof window === 'undefined') return NAV_DEFAULT_WIDTH
  const raw = window.localStorage.getItem(WIDTH_STORAGE_KEY)
  if (!raw) return NAV_DEFAULT_WIDTH
  const n = Number(raw)
  return clampWidth(n)
}

export default function Layout({ children }: { children: ReactNode }) {
  const location = useLocation()
  const navigate = useNavigate()
  const kiosk = new URLSearchParams(location.search).get('nochrome') === '1'

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return window.localStorage.getItem(STORAGE_KEY) === '1'
  })
  const [helpOpen, setHelpOpen] = useState(false)
  const [navWidth, setNavWidth] = useState<number>(() => loadWidth())

  useEffect(() => {
    window.localStorage.setItem(STORAGE_KEY, collapsed ? '1' : '0')
  }, [collapsed])

  useEffect(() => {
    window.localStorage.setItem(WIDTH_STORAGE_KEY, String(navWidth))
  }, [navWidth])

  const pages = usePages()
  const { files } = useFiles()

  // j/k and digit shortcuts traverse pages in the same visual order the sidebar
  // renders (folders > indexPage, then children, then sibling pages — all
  // alphabetical at each level). Computing this from the content tree keeps
  // keyboard navigation and sidebar ordering in lockstep.
  const orderedHrefs = useMemo(
    () => flattenContentTreePageHrefs(buildContentTree(pages, files)),
    [pages, files]
  )

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
      c: () => {
        void copyPageSource(pagePathFromLocation(location.pathname))
      },
    }

    if (orderedHrefs.length > 0) {
      const currentIdx = orderedHrefs.indexOf(location.pathname)
      const wrap = (i: number) => (i + orderedHrefs.length) % orderedHrefs.length
      map.j = () => {
        const next = currentIdx < 0 ? 0 : wrap(currentIdx + 1)
        navigate(orderedHrefs[next])
      }
      map.k = () => {
        const prev = currentIdx < 0 ? orderedHrefs.length - 1 : wrap(currentIdx - 1)
        navigate(orderedHrefs[prev])
      }
      for (let i = 0; i < Math.min(9, orderedHrefs.length); i++) {
        const digit = String(i + 1)
        const target = orderedHrefs[i]
        map[digit] = () => navigate(target)
      }
    }

    return map
  }, [helpOpen, orderedHrefs, location.pathname, navigate])

  useKeyboardShortcuts(shortcuts, !kiosk)

  const showNav = !kiosk && !collapsed

  return (
    <div className="min-h-screen flex" style={{ background: 'var(--bg)' }}>
      {showNav && (
        <Nav
          pages={pages}
          width={navWidth}
          onResize={w => setNavWidth(clampWidth(w))}
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
      {!kiosk && <GrabTray />}
      <CopyToast />
    </div>
  )
}
