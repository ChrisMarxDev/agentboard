import { Link } from 'react-router-dom'
import { Sparkles, SquareCheckBig } from 'lucide-react'
import type { ContentTreeNode } from '../../lib/contentTree'
import { NewProjectButton } from './Nav'

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
// Why: Tasks and Skills are first-level citizens per concept.md §4 but
// they're also normal pages in the tree (a `tasks/` folder of cards, a
// `skills/` folder of skill manifests). Hoisting them here gives the
// "feels like a real destination" treatment without duplicating the
// data — there's still only one tasks page on disk, the same one.
const HOISTED_FOLDERS: { name: string; icon: typeof Sparkles }[] = [
  { name: 'tasks', icon: SquareCheckBig },
  { name: 'skills', icon: Sparkles },
]

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
  // At the root level only, pull HOISTED_FOLDERS to the top in declared
  // order. Deeper levels render in whatever order materialize() chose.
  const ordered = depth === 0
    ? [...nodes].sort((a, b) => {
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
            {isOpen && depth === 0 && node.name === 'tasks' && (
              <div style={{ paddingLeft: ROW_PADDING_X + (depth + 1) * INDENT_STEP + CHEVRON_WIDTH }}>
                <NewProjectButton />
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
