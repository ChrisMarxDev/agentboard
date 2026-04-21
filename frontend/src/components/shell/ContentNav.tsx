import { Link } from 'react-router-dom'
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

export default function ContentNav({
  nodes,
  depth,
  expanded,
  onToggle,
  onExpand,
  activePath,
}: ContentNavProps) {
  return (
    <div className="flex flex-col gap-1">
      {nodes.map(node => {
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
                className="flex-1 flex items-center truncate py-1.5"
                style={{ color: 'inherit', paddingLeft: 4 }}
              >
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
