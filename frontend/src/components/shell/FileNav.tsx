import { Link } from 'react-router-dom'
import type { FileTreeNode } from '../../lib/fileTree'

interface FileNavProps {
  nodes: FileTreeNode[]
  depth: number
  expanded: Set<string>
  onToggle: (folderPath: string) => void
  activePath: string
}

const ROW_PADDING_X = 12
const INDENT_STEP = 12
const CHEVRON_WIDTH = 20

export default function FileNav({
  nodes,
  depth,
  expanded,
  onToggle,
  activePath,
}: FileNavProps) {
  return (
    <div className="flex flex-col gap-1">
      {nodes.map(node => {
        const indent = depth * INDENT_STEP

        if (node.kind === 'file') {
          const href = `/files/${node.path}`
          const isActive = activePath === href
          return (
            <Link
              key={node.path}
              to={href}
              className="flex items-center py-1.5 rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent + CHEVRON_WIDTH,
                paddingRight: ROW_PADDING_X,
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
              }}
            >
              <span className="truncate">{node.name}</span>
            </Link>
          )
        }

        const isOpen = expanded.has(node.path)
        const chevronLabel = `${isOpen ? 'Collapse' : 'Expand'} ${node.name}`

        return (
          <div key={`folder:${node.path}`} className="flex flex-col gap-1">
            <div
              className="flex items-stretch rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent,
                paddingRight: ROW_PADDING_X,
                color: 'var(--text-secondary)',
                minHeight: '2rem',
              }}
            >
              <button
                type="button"
                onClick={() => onToggle(node.path)}
                aria-label={chevronLabel}
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
              <button
                type="button"
                onClick={() => onToggle(node.path)}
                className="flex-1 flex items-center text-left truncate py-1.5"
                style={{
                  background: 'transparent',
                  border: 'none',
                  color: 'inherit',
                  cursor: 'pointer',
                  paddingLeft: 4,
                }}
              >
                <span className="truncate">{node.name}</span>
              </button>
            </div>
            {isOpen && (
              <FileNav
                nodes={node.children}
                depth={depth + 1}
                expanded={expanded}
                onToggle={onToggle}
                activePath={activePath}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}
