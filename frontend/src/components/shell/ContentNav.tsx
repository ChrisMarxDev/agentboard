import { Link } from 'react-router-dom'
import { Sparkles } from 'lucide-react'
import type { ContentTreeNode } from '../../lib/contentTree'

interface ContentNavProps {
  nodes: ContentTreeNode[]
  depth: number
  expanded: Set<string>
  onToggle: (folderPath: string) => void
  onExpand: (folderPath: string) => void
  activePath: string
}

const ROW_PADDING_X = 12
const INDENT_STEP = 12
const CHEVRON_WIDTH = 20

// HOISTED_FOLDERS sort to the top of the root level and render with an
// icon. The order in the array IS the display order. Folders not in
// this list fall back to alphabetical.
//
// Skills stays hoisted — the skills folder IS the agent docs catalog
// and benefits from a sticky top slot. Tasks used to be hoisted too
// but lost its place: the `tasks/` tree was a placeholder destination
// for kanban projects and cluttered the sidebar with implementation
// detail. Project pages still live on disk under `tasks/`, just hidden
// from the sidebar (see HIDDEN_ROOT_FOLDERS below) — the user can
// reach them via direct URL or by surfacing them inside other pages.
const HOISTED_FOLDERS: { name: string; icon: typeof Sparkles }[] = [
  { name: 'skills', icon: Sparkles },
]

// HIDDEN_ROOT_FOLDERS are filtered out of the sidebar entirely at the
// root level. The pages still exist and route normally — they just
// don't appear in the navigation tree.
const HIDDEN_ROOT_FOLDERS = new Set<string>(['tasks'])

function hoistOrder(name: string): number {
  const i = HOISTED_FOLDERS.findIndex(h => h.name === name)
  return i === -1 ? Number.MAX_SAFE_INTEGER : i
}

function iconForFolder(name: string) {
  return HOISTED_FOLDERS.find(h => h.name === name)?.icon
}

export default function ContentNav({
  nodes,
  depth,
  expanded,
  onToggle,
  onExpand,
  activePath,
}: ContentNavProps) {
  // At the root level only, hide HIDDEN_ROOT_FOLDERS and pull
  // HOISTED_FOLDERS to the top. Deeper levels render in whatever
  // order materialize() chose.
  const ordered = depth === 0
    ? [...nodes]
        .filter(n => !(n.kind === 'folder' && HIDDEN_ROOT_FOLDERS.has(n.name)))
        .sort((a, b) => {
          const ai = a.kind === 'folder' ? hoistOrder(a.name) : Number.MAX_SAFE_INTEGER
          const bi = b.kind === 'folder' ? hoistOrder(b.name) : Number.MAX_SAFE_INTEGER
          if (ai !== bi) return ai - bi
          return 0
        })
    : nodes
  return (
    <div className="flex flex-col gap-1">
      {ordered.map(node => {
        const indent = depth * INDENT_STEP

        if (node.kind === 'page' || node.kind === 'file') {
          const isActive = activePath === node.href
          const label = node.kind === 'page' ? node.title : node.name
          return (
            <Link
              key={`${node.kind}:${node.href}`}
              to={node.href}
              className="flex items-center py-1.5 rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent + CHEVRON_WIDTH,
                paddingRight: ROW_PADDING_X,
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive
                  ? 'var(--accent)'
                  : node.kind === 'file'
                    ? 'var(--text-secondary)'
                    : 'var(--text-secondary)',
                fontStyle: node.kind === 'file' ? 'normal' : 'normal',
                opacity: node.kind === 'file' ? 0.9 : 1,
              }}
            >
              <span className="truncate">{label}</span>
            </Link>
          )
        }

        const isOpen = expanded.has(node.path)
        const isActive = activePath === node.href

        return (
          <div key={`folder:${node.path}`} className="flex flex-col gap-1">
            <div
              className="flex items-stretch rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent,
                paddingRight: ROW_PADDING_X,
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                minHeight: '2rem',
              }}
            >
              <button
                type="button"
                onClick={e => {
                  e.preventDefault()
                  e.stopPropagation()
                  onToggle(node.path)
                }}
                aria-label={`${isOpen ? 'Collapse' : 'Expand'} ${node.title}`}
                aria-expanded={isOpen}
                className="flex items-center justify-center shrink-0"
                style={{
                  width: CHEVRON_WIDTH,
                  alignSelf: 'stretch',
                  background: 'transparent',
                  border: 'none',
                  color: 'inherit',
                  cursor: 'pointer',
                  padding: 0,
                }}
              >
                <span aria-hidden className="text-xs leading-none" style={{ opacity: 0.75 }}>
                  {isOpen ? '▼' : '▶'}
                </span>
              </button>
              <Link
                to={node.href}
                onClick={() => onExpand(node.path)}
                className="flex-1 flex items-center truncate py-1.5 gap-1.5"
                style={{ color: 'inherit', paddingLeft: 4 }}
              >
                {(() => {
                  const Icon = depth === 0 ? iconForFolder(node.name) : undefined
                  return Icon ? <Icon size={14} strokeWidth={2} /> : null
                })()}
                <span className="truncate">{node.title}</span>
              </Link>
            </div>
            {isOpen && (
              <ContentNav
                nodes={node.children}
                depth={depth + 1}
                expanded={expanded}
                onToggle={onToggle}
                onExpand={onExpand}
                activePath={activePath}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}
