import { Link } from 'react-router-dom'
import { BookOpen, ChevronRight } from 'lucide-react'
import type { TreeNode } from '../../lib/pageTree'

interface NavTreeProps {
  nodes: TreeNode[]
  depth: number
  expanded: Set<string>
  onToggle: (folderPath: string) => void
  onExpand: (folderPath: string) => void
  activePath: string
}

const ROW_PADDING_X = 12
const INDENT_STEP = 12
const CHEVRON_WIDTH = 20

// Any folder or page whose own path segment is literally "skills" gets a
// book icon. Skills are a first-class agent-install surface in AgentBoard,
// so the convention is enforced visually across every board.
function isSkillsNode(path: string): boolean {
  const seg = path.split('/').filter(Boolean).pop() ?? ''
  return seg.toLowerCase() === 'skills'
}

function SkillsIcon() {
  return (
    <BookOpen
      size={14}
      aria-hidden
      className="shrink-0"
      style={{ marginRight: 6, opacity: 0.85 }}
    />
  )
}

export default function NavTree({
  nodes,
  depth,
  expanded,
  onToggle,
  onExpand,
  activePath,
}: NavTreeProps) {
  return (
    <div className="flex flex-col gap-1">
      {nodes.map(node => {
        const indent = depth * INDENT_STEP

        if (node.kind === 'page') {
          const page = node.page
          const isActive = activePath === page.path
          const showSkillsIcon = isSkillsNode(page.path)
          return (
            <Link
              key={page.path}
              to={page.path}
              className="flex items-center py-2 rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent + CHEVRON_WIDTH,
                paddingRight: ROW_PADDING_X,
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
              }}
            >
              {showSkillsIcon && <SkillsIcon />}
              <span className="truncate">{page.title}</span>
            </Link>
          )
        }

        const isOpen = expanded.has(node.path)
        const indexPage = node.indexPage
        const isActive = indexPage != null && activePath === indexPage.path
        const chevronLabel = `${isOpen ? 'Collapse' : 'Expand'} ${node.name}`

        const onChevronClick = (e: React.MouseEvent) => {
          e.preventDefault()
          e.stopPropagation()
          onToggle(node.path)
        }

        const onNameClick = (e: React.MouseEvent) => {
          if (isActive && isOpen) {
            e.preventDefault()
            onToggle(node.path)
            return
          }
          onExpand(node.path)
        }

        const chevronButton = (
          <button
            type="button"
            onClick={onChevronClick}
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
              font: 'inherit',
            }}
          >
            <ChevronRight
              size={14}
              aria-hidden
              style={{
                opacity: 0.75,
                transition: 'transform 180ms ease',
                transform: isOpen ? 'rotate(90deg)' : 'rotate(0deg)',
              }}
            />
          </button>
        )

        return (
          <div key={`folder:${node.path}`} className="flex flex-col gap-1">
            <div
              className="flex items-stretch rounded-md text-sm transition-colors"
              style={{
                paddingLeft: ROW_PADDING_X + indent,
                paddingRight: ROW_PADDING_X,
                background: isActive ? 'var(--accent-light)' : 'transparent',
                color: isActive ? 'var(--accent)' : 'var(--text-secondary)',
                minHeight: '2.25rem',
              }}
            >
              {chevronButton}
              {indexPage ? (
                <Link
                  to={indexPage.path}
                  onClick={onNameClick}
                  className="flex-1 flex items-center truncate py-2"
                  style={{ color: 'inherit', paddingLeft: 4 }}
                >
                  {isSkillsNode(node.path) && <SkillsIcon />}
                  <span className="truncate">{node.name}</span>
                </Link>
              ) : (
                <button
                  type="button"
                  onClick={() => onExpand(node.path)}
                  className="flex-1 flex items-center text-left truncate py-2"
                  style={{
                    background: 'transparent',
                    border: 'none',
                    color: 'inherit',
                    cursor: 'pointer',
                    font: 'inherit',
                    paddingLeft: 4,
                  }}
                >
                  {isSkillsNode(node.path) && <SkillsIcon />}
                  <span className="truncate">{node.name}</span>
                </button>
              )}
            </div>
            <div
              className="grid"
              aria-hidden={!isOpen}
              style={{
                gridTemplateRows: isOpen ? '1fr' : '0fr',
                transition: 'grid-template-rows 180ms ease',
              }}
            >
              <div style={{ minHeight: 0, overflow: 'hidden' }}>
                <div style={{ paddingTop: 4 }}>
                  <NavTree
                    nodes={node.children}
                    depth={depth + 1}
                    expanded={expanded}
                    onToggle={onToggle}
                    onExpand={onExpand}
                    activePath={activePath}
                  />
                </div>
              </div>
            </div>
          </div>
        )
      })}
    </div>
  )
}
