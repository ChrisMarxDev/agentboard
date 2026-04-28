import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { Home as HomeIcon, Inbox as InboxIcon, LogOut, Magnet, Search, ShieldCheck, SquareCheckBig, X } from 'lucide-react'
import { clearToken, getToken, redirectToLogin } from '../../lib/session'
import { ThemeSwitch } from './ThemeSwitch'
import Kbd from './Kbd'
import ContentNav from './ContentNav'
import {
  ancestorFolderPathsForHref,
  buildContentTree,
  collectContentFolderPaths,
  filterContentTree,
} from '../../lib/contentTree'
import type { PageEntry } from '../../hooks/usePages'
import { useGrab } from '../../hooks/useGrab'
import { setMode } from '../../lib/grab'
import { useFiles } from '../../hooks/useFiles'
import { useContentSearch } from '../../hooks/useContentSearch'

const EXPANDED_STORAGE_KEY = 'agentboard:nav-expanded'

export const NAV_MIN_WIDTH = 200
export const NAV_MAX_WIDTH = 600
export const NAV_DEFAULT_WIDTH = 224

function loadExpanded(): Set<string> {
  if (typeof window === 'undefined') return new Set()
  try {
    const raw = window.localStorage.getItem(EXPANDED_STORAGE_KEY)
    if (!raw) return new Set()
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) return new Set(parsed.filter(x => typeof x === 'string'))
  } catch {
    // ignore corrupt storage
  }
  return new Set()
}

interface NavProps {
  pages: PageEntry[]
  width: number
  onResize?: (width: number) => void
  onCollapse?: () => void
  onOpenHelp?: () => void
}

export default function Nav({ pages, width, onResize, onCollapse, onOpenHelp }: NavProps) {
  const location = useLocation()
  const { mode: grabMode, picks } = useGrab()
  const { files } = useFiles()

  const fullTree = useMemo(() => buildContentTree(pages, files), [pages, files])
  const folderPathsSet = useMemo(() => new Set(collectContentFolderPaths(fullTree)), [fullTree])

  const [query, setQuery] = useState('')
  const searchInputRef = useRef<HTMLInputElement | null>(null)

  const { tree, searchExpanded } = useMemo(() => {
    const { nodes, expandedPaths } = filterContentTree(fullTree, query)
    return { tree: nodes, searchExpanded: expandedPaths }
  }, [fullTree, query])

  // When title/path filtering returns no matches, escalate to server-side
  // full-text search over page content. Debounced to keep keystrokes cheap.
  const titleMatchEmpty = query.trim() !== '' && tree.length === 0
  const { hits: contentHits, ready: contentSearchReady } = useContentSearch(query, titleMatchEmpty)

  const [expanded, setExpanded] = useState<Set<string>>(() => loadExpanded())

  useEffect(() => {
    const toOpen = [...ancestorFolderPathsForHref(location.pathname)]
    const trimmed = location.pathname.replace(/^\/+/, '').replace(/\/+$/, '')
    if (trimmed && folderPathsSet.has(trimmed)) toOpen.push(trimmed)
    if (toOpen.length === 0) return
    setExpanded(prev => {
      let changed = false
      const next = new Set(prev)
      for (const a of toOpen) {
        if (!next.has(a)) {
          next.add(a)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }, [location.pathname, folderPathsSet])

  useEffect(() => {
    try {
      window.localStorage.setItem(EXPANDED_STORAGE_KEY, JSON.stringify(Array.from(expanded)))
    } catch {
      // storage may be unavailable (private mode); silently ignore
    }
  }, [expanded])

  const onToggle = (folderPath: string) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(folderPath)) next.delete(folderPath)
      else next.add(folderPath)
      return next
    })
  }

  const onExpand = (folderPath: string) => {
    setExpanded(prev => {
      if (prev.has(folderPath)) return prev
      const next = new Set(prev)
      next.add(folderPath)
      return next
    })
  }

  const navRef = useRef<HTMLElement | null>(null)
  const [isDragging, setIsDragging] = useState(false)

  // `/` focuses the search input (but not when the user is already typing in
  // an input elsewhere). Escape inside the search clears the query.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/') return
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
      e.preventDefault()
      searchInputRef.current?.focus()
      searchInputRef.current?.select()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (!isDragging || !onResize) return
    const onMove = (e: MouseEvent) => {
      const left = navRef.current?.getBoundingClientRect().left ?? 0
      const next = Math.max(NAV_MIN_WIDTH, Math.min(NAV_MAX_WIDTH, e.clientX - left))
      onResize(next)
    }
    const onUp = () => setIsDragging(false)
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    document.body.style.cursor = 'col-resize'
    document.body.style.userSelect = 'none'
    return () => {
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }
  }, [isDragging, onResize])

  return (
    <nav
      ref={navRef}
      className="shrink-0 border-r p-4 flex flex-col gap-1 h-screen sticky top-0 relative"
      style={{
        width,
        borderColor: 'var(--border)',
        background: 'var(--bg-secondary)',
      }}
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

      <TopNavItems activePath={location.pathname} />

      <div
        className="flex items-center mb-2"
        style={{
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          borderRadius: '0.5rem',
          padding: '0 0.5rem',
        }}
      >
        <Search size={13} style={{ color: 'var(--text-secondary)', flexShrink: 0 }} />
        <input
          ref={searchInputRef}
          type="text"
          value={query}
          onChange={e => setQuery(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Escape') {
              setQuery('')
              ;(e.target as HTMLInputElement).blur()
            }
          }}
          placeholder="Search"
          aria-label="Search content"
          className="flex-1 min-w-0 h-7 text-sm"
          style={{
            background: 'transparent',
            border: 'none',
            outline: 'none',
            color: 'var(--text)',
            padding: '0 0.4rem',
          }}
        />
        {query ? (
          <button
            type="button"
            onClick={() => setQuery('')}
            aria-label="Clear search"
            title="Clear"
            className="flex items-center justify-center"
            style={{
              width: 18,
              height: 18,
              background: 'transparent',
              border: 'none',
              color: 'var(--text-secondary)',
              cursor: 'pointer',
              flexShrink: 0,
            }}
          >
            <X size={12} />
          </button>
        ) : (
          <Kbd>/</Kbd>
        )}
      </div>

      <div
        className="flex items-center justify-between px-3 pb-2 text-[10px] uppercase tracking-wide"
        style={{ color: 'var(--text-secondary)' }}
      >
        <span>{query ? `Matches for "${query}"` : 'Content'}</span>
        <div className="flex items-center gap-1">
          <Kbd>J</Kbd>
          <Kbd>K</Kbd>
        </div>
      </div>

      <div className="flex-1 flex flex-col gap-1 overflow-y-auto">
        {titleMatchEmpty && !contentSearchReady && (
          <div className="px-3 py-2 text-sm" style={{ color: 'var(--text-secondary)' }}>
            Searching page content…
          </div>
        )}
        {titleMatchEmpty && contentSearchReady && contentHits.length === 0 && (
          <div className="px-3 py-2 text-sm" style={{ color: 'var(--text-secondary)' }}>
            No matches.
          </div>
        )}
        {titleMatchEmpty && contentHits.length > 0 && (
          <div className="flex flex-col gap-1">
            <div
              className="px-3 pb-1 text-[10px] uppercase tracking-wide"
              style={{ color: 'var(--text-secondary)' }}
            >
              Found in page content
            </div>
            {contentHits.map(h => (
              <Link
                key={h.path}
                to={h.path}
                className="flex flex-col px-3 py-2 rounded-md text-sm gap-0.5"
                style={{ color: 'var(--text)' }}
              >
                <span className="truncate font-medium">{h.title || h.path}</span>
                <span
                  className="text-xs truncate"
                  style={{ color: 'var(--text-secondary)' }}
                  // snippet is server-rendered with <mark>...</mark> wrappers
                  // around the match; interpret as HTML so the highlight
                  // shows. The snippet content comes from our own DB so XSS
                  // exposure is bounded by who can write pages.
                  dangerouslySetInnerHTML={{ __html: h.snippet }}
                />
              </Link>
            ))}
          </div>
        )}
        <ContentNav
          nodes={tree}
          depth={0}
          expanded={query ? searchExpanded : expanded}
          onToggle={onToggle}
          onExpand={onExpand}
          activePath={location.pathname}
        />
      </div>

      <SystemNavItems activePath={location.pathname} />

      <div className="flex items-center gap-2 pt-2">
        <ThemeSwitch />
        <button
          onClick={() => setMode(!grabMode)}
          aria-label={grabMode ? 'Leave grab mode' : 'Enter grab mode'}
          aria-pressed={grabMode}
          title={grabMode ? 'Grab mode — click cards to pick' : 'Grab mode — pick cards across pages to paste into an agent'}
          className="h-8 flex items-center justify-center rounded-md px-2"
          style={{
            background: grabMode ? 'var(--accent-light)' : 'var(--bg)',
            border: `1px solid ${grabMode ? 'var(--accent)' : 'var(--border)'}`,
            color: grabMode ? 'var(--accent)' : 'var(--text-secondary)',
            cursor: 'pointer',
            fontSize: '0.875rem',
            fontWeight: 500,
            position: 'relative',
          }}
        >
          <Magnet size={16} strokeWidth={2} />
          {grabMode && picks.length > 0 && (
            <span
              style={{
                position: 'absolute',
                top: '-4px',
                right: '-4px',
                minWidth: '16px',
                height: '16px',
                padding: '0 4px',
                fontSize: '10px',
                lineHeight: '16px',
                fontWeight: 700,
                color: 'white',
                background: 'var(--accent)',
                borderRadius: '9999px',
                textAlign: 'center',
              }}
            >
              {picks.length}
            </span>
          )}
        </button>
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
        <SignOutButton />
      </div>

      {onResize && (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize navigation"
          onMouseDown={e => {
            e.preventDefault()
            setIsDragging(true)
          }}
          onDoubleClick={() => onResize(NAV_DEFAULT_WIDTH)}
          title="Drag to resize · double-click to reset"
          className="absolute top-0 right-0 h-full"
          style={{
            width: 6,
            marginRight: -3,
            cursor: 'col-resize',
            background: isDragging ? 'var(--accent)' : 'transparent',
            opacity: isDragging ? 0.4 : 1,
            transition: isDragging ? 'none' : 'background 120ms',
            zIndex: 10,
          }}
          onMouseEnter={e => {
            if (!isDragging) e.currentTarget.style.background = 'var(--border)'
          }}
          onMouseLeave={e => {
            if (!isDragging) e.currentTarget.style.background = 'transparent'
          }}
        />
      )}
    </nav>
  )
}

// SignOutButton clears the stored token and redirects to /login. Hidden
// when there's no token (open-mode / loopback install) so we don't show a
// sign-out affordance that means nothing.
function SignOutButton() {
  if (!getToken()) return null
  return (
    <button
      onClick={() => {
        clearToken()
        redirectToLogin()
      }}
      aria-label="Sign out"
      title="Sign out"
      className="h-8 flex items-center justify-center rounded-md px-2"
      style={{
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        color: 'var(--text-secondary)',
        cursor: 'pointer',
      }}
    >
      <LogOut size={14} />
    </button>
  )
}

// TopNavItems are the three "where do I GO right now" destinations:
// Inbox · Home · Tasks. Sits above the search box; the page tree below
// the search box answers "where is THAT doc". Skills and the rest of
// the folder tree live in the page tree, not as separate slots.
//
// See `concept.md` §4 and `/concept-rollout` Phase A.
interface TopSlot {
  href: string
  label: string
  icon: React.ComponentType<{ size?: number; strokeWidth?: number }>
}

const TOP_SLOTS: TopSlot[] = [
  { href: '/inbox', label: 'Inbox', icon: InboxIcon },
  { href: '/',      label: 'Home',  icon: HomeIcon },
  { href: '/tasks', label: 'Tasks', icon: SquareCheckBig },
]

function TopNavItems({ activePath }: { activePath: string }) {
  return (
    <div className="flex flex-col gap-0.5 mb-3">
      {TOP_SLOTS.map(slot => {
        // Exact-match for /inbox and /tasks so /inbox/some-future-subpage
        // doesn't bleed into Inbox's highlight, AND /tasks/<id> highlights
        // the Tasks slot since per-task pages are conceptually under it.
        // / (Home) is exact-only — every other path would otherwise match.
        const active =
          slot.href === '/'
            ? activePath === '/'
            : activePath === slot.href || activePath.startsWith(slot.href + '/')
        return (
          <Link
            key={slot.href}
            to={slot.href}
            className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm"
            style={{
              background: active ? 'var(--accent-light)' : 'transparent',
              color: active ? 'var(--accent)' : 'var(--text)',
              fontWeight: active ? 600 : 500,
              textDecoration: 'none',
            }}
            onMouseEnter={e => {
              if (!active) e.currentTarget.style.background = 'var(--bg)'
            }}
            onMouseLeave={e => {
              if (!active) e.currentTarget.style.background = 'transparent'
            }}
          >
            <slot.icon size={14} strokeWidth={2} />
            <span>{slot.label}</span>
          </Link>
        )
      })}
    </div>
  )
}

// SystemNavItems holds the non-content "destinations" (currently just Auth).
// Kept outside the content tree so page-search doesn't affect it and so the
// styling reads as a distinct section. Sits above the utility-icon row.
function SystemNavItems({ activePath }: { activePath: string }) {
  const active = activePath === '/admin' || activePath.startsWith('/admin/')
  return (
    <div className="flex flex-col gap-1 pt-2 pb-1 border-t" style={{ borderColor: 'var(--border)' }}>
      <Link
        to="/admin"
        className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm"
        style={{
          background: active ? 'var(--accent-light)' : 'transparent',
          color: active ? 'var(--accent)' : 'var(--text-secondary)',
          fontWeight: active ? 600 : 500,
          textDecoration: 'none',
        }}
        onMouseEnter={e => {
          if (!active) e.currentTarget.style.background = 'var(--bg)'
        }}
        onMouseLeave={e => {
          if (!active) e.currentTarget.style.background = 'transparent'
        }}
      >
        <ShieldCheck size={14} />
        <span>Auth</span>
      </Link>
    </div>
  )
}
