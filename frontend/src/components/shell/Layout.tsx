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
import { UserMenu } from './UserMenu'
import CopyToast from './CopyToast'
import { copyPageSource, pagePathFromLocation } from '../../lib/copyPage'

const STORAGE_KEY = 'agentboard:nav-collapsed'
const WIDTH_STORAGE_KEY = 'agentboard:nav-width'
const MOBILE_BREAKPOINT = 768 // px — matches Tailwind's `md`

function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(() => {
    if (typeof window === 'undefined') return false
    return window.innerWidth < MOBILE_BREAKPOINT
  })
  useEffect(() => {
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT - 1}px)`)
    const onChange = (e: MediaQueryListEvent) => setIsMobile(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])
  return isMobile
}

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

  // When the page is embedded (kiosk mode), post height updates to the
  // parent window so iframe wrappers can auto-resize. The parent
  // listens for {type: "agentboard:embed:height", height} on the
  // message channel. No-op when not in kiosk mode.
  useEffect(() => {
    if (!kiosk || typeof window === 'undefined') return
    const post = () => {
      const h = document.documentElement.scrollHeight
      try {
        window.parent?.postMessage({ type: 'agentboard:embed:height', height: h }, '*')
      } catch {
        // parent refused — nothing we can do
      }
    }
    post()
    const ro = new ResizeObserver(post)
    ro.observe(document.documentElement)
    return () => ro.disconnect()
  }, [kiosk])
  const isMobile = useIsMobile()

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    // Default collapsed on mobile regardless of prior persistence — a
    // desktop user resizing to mobile shouldn't suddenly have half the
    // viewport covered by a sidebar.
    if (window.innerWidth < MOBILE_BREAKPOINT) return true
    return window.localStorage.getItem(STORAGE_KEY) === '1'
  })
  const [helpOpen, setHelpOpen] = useState(false)
  const [navWidth, setNavWidth] = useState<number>(() => loadWidth())

  // On breakpoint transitions: close on mobile so the content is visible
  // first; open on desktop unless the user explicitly collapsed it before
  // the mobile-auto-close fired. Checking the stored value rather than the
  // current `collapsed` avoids losing the user's desktop preference when
  // they flip between sizes.
  useEffect(() => {
    if (isMobile) {
      setCollapsed(true)
    } else if (typeof window !== 'undefined') {
      setCollapsed(window.localStorage.getItem(STORAGE_KEY) === '1')
    }
  }, [isMobile])

  // On mobile, auto-close the drawer on route change — tapping a link should
  // reveal the destination page, not leave the menu on top of it.
  useEffect(() => {
    if (isMobile) setCollapsed(true)
  }, [location.pathname, isMobile])

  useEffect(() => {
    // Only persist the user's collapse preference on desktop. Mobile
    // auto-closes the drawer; persisting that would mean a desktop user
    // who happened to be on a phone earlier finds the sidebar hidden on
    // their laptop next session.
    if (isMobile) return
    window.localStorage.setItem(STORAGE_KEY, collapsed ? '1' : '0')
  }, [collapsed, isMobile])

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

  const drawerOpen = !kiosk && !collapsed

  return (
    <div className="min-h-screen flex relative" style={{ background: 'var(--bg)' }}>
      {!kiosk && (
        <div
          aria-hidden={!drawerOpen || !isMobile}
          className={isMobile ? 'fixed inset-y-0 left-0 z-40' : 'sticky top-0 self-start shrink-0'}
          style={{
            transform: isMobile
              ? drawerOpen ? 'translateX(0)' : 'translateX(-100%)'
              : undefined,
            transition: isMobile ? 'transform 180ms ease-out' : undefined,
            pointerEvents: isMobile && !drawerOpen ? 'none' : undefined,
          }}
        >
          {(drawerOpen || isMobile) && (
            <Nav
              pages={pages}
              width={isMobile ? Math.min(320, typeof window !== 'undefined' ? window.innerWidth - 48 : 320) : navWidth}
              onResize={isMobile ? undefined : w => setNavWidth(clampWidth(w))}
              onCollapse={() => setCollapsed(true)}
              onOpenHelp={() => setHelpOpen(true)}
            />
          )}
        </div>
      )}

      {/* Mobile backdrop — tap to dismiss. */}
      {!kiosk && isMobile && drawerOpen && (
        <button
          aria-label="Close navigation"
          onClick={() => setCollapsed(true)}
          className="fixed inset-0 z-30 bg-black/40"
          style={{ border: 'none' }}
        />
      )}

      <main className="flex-1 p-4 md:p-8 max-w-5xl mx-auto w-full relative">
        {!kiosk && (isMobile || collapsed) && !drawerOpen && (
          <button
            onClick={() => setCollapsed(false)}
            aria-label="Show navigation"
            title="Show navigation"
            className="fixed top-3 left-3 z-50 flex items-center gap-1.5 rounded-md px-2"
            style={{
              minHeight: 44,
              minWidth: 44,
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border)',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
            }}
          >
            <span className="text-lg leading-none">☰</span>
            {!isMobile && <Kbd>B</Kbd>}
          </button>
        )}
        {children}
      </main>
      <ShortcutsHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
      {!kiosk && <GrabTray />}
      {!kiosk && <UserMenu />}
      <CopyToast />
    </div>
  )
}
