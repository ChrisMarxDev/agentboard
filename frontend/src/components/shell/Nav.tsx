import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { Magnet } from 'lucide-react'
import { ThemeSwitch } from './ThemeSwitch'
import Kbd from './Kbd'
import ContentNav from './ContentNav'
import {
  ancestorFolderPathsForHref,
  buildContentTree,
  collectContentFolderPaths,
} from '../../lib/contentTree'
import type { PageEntry } from '../../hooks/usePages'
import { useGrab } from '../../hooks/useGrab'
import { setMode } from '../../lib/grab'
import { useFiles } from '../../hooks/useFiles'

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

  const tree = useMemo(() => buildContentTree(pages, files), [pages, files])
  const folderPathsSet = useMemo(() => new Set(collectContentFolderPaths(tree)), [tree])

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

      <div
        className="flex items-center justify-between px-3 pb-2 text-[10px] uppercase tracking-wide"
        style={{ color: 'var(--text-secondary)' }}
      >
        <span>Content</span>
        <div className="flex items-center gap-1">
          <Kbd>J</Kbd>
          <Kbd>K</Kbd>
        </div>
      </div>

      <div className="flex-1 flex flex-col gap-1 overflow-y-auto">
        <ContentNav
          nodes={tree}
          depth={0}
          expanded={expanded}
          onToggle={onToggle}
          onExpand={onExpand}
          activePath={location.pathname}
        />
      </div>

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
