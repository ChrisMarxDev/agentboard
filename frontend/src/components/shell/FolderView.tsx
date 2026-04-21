import { Link } from 'react-router-dom'
import { Folder as FolderIcon, FileText, File as FileIcon } from 'lucide-react'
import type { ContentFolder, ContentTreeNode } from '../../lib/contentTree'

/**
 * Auto-generated landing page for a folder that has no sibling index `.md`.
 * Every folder is browsable this way, and dropping a real `<folder>.md`
 * overrides the view (progressive enhancement, no migration needed).
 */
export default function FolderView({ folder }: { folder: ContentFolder }) {
  const children = folder.children
  const title = folder.title
  const subtitle = folder.path ? `${folder.path}/` : ''

  return (
    <div className="relative">
      <header className="mb-6">
        <div className="text-xs mb-1" style={{ color: 'var(--text-secondary)' }}>
          {subtitle}
        </div>
        <h1 className="text-2xl font-semibold" style={{ color: 'var(--text)' }}>
          {title}
        </h1>
        <p className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
          {childCountLabel(children)} — drop a <code>{folder.name || 'folder'}.md</code> in this
          folder to replace this auto-generated index.
        </p>
      </header>

      {children.length === 0 ? (
        <div
          className="p-4 rounded-md text-sm"
          style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)' }}
        >
          Empty folder.
        </div>
      ) : (
        <ul className="flex flex-col gap-2">
          {children.map(renderChild)}
        </ul>
      )}
    </div>
  )
}

function renderChild(node: ContentTreeNode) {
  const { href, title, subtitle } = childMeta(node)
  const Icon = iconFor(node)
  return (
    <li key={keyFor(node)}>
      <Link
        to={href}
        className="flex items-start gap-3 rounded-md p-3 transition-colors"
        style={{
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border)',
          color: 'var(--text)',
          textDecoration: 'none',
        }}
      >
        <Icon size={16} style={{ marginTop: 2, color: 'var(--text-secondary)', flexShrink: 0 }} />
        <div className="min-w-0 flex-1">
          <div className="font-medium truncate">{title}</div>
          {subtitle && (
            <div className="text-xs mt-0.5" style={{ color: 'var(--text-secondary)' }}>
              {subtitle}
            </div>
          )}
        </div>
      </Link>
    </li>
  )
}

function childMeta(node: ContentTreeNode): {
  href: string
  title: string
  subtitle?: string
} {
  if (node.kind === 'folder') {
    return { href: node.href, title: node.title, subtitle: `${childCountLabel(node.children)} · folder` }
  }
  if (node.kind === 'page') {
    return { href: node.href, title: node.title, subtitle: 'page' }
  }
  return {
    href: node.href,
    title: node.name,
    subtitle: node.entry.content_type + ' · ' + formatSize(node.entry.size),
  }
}

function iconFor(node: ContentTreeNode) {
  if (node.kind === 'folder') return FolderIcon
  if (node.kind === 'page') return FileText
  return FileIcon
}

function keyFor(node: ContentTreeNode): string {
  if (node.kind === 'folder') return `folder:${node.path}`
  return `${node.kind}:${node.href}`
}

function childCountLabel(children: ContentTreeNode[]): string {
  let folders = 0
  let pages = 0
  let files = 0
  for (const c of children) {
    if (c.kind === 'folder') folders++
    else if (c.kind === 'page') pages++
    else files++
  }
  const parts: string[] = []
  if (folders) parts.push(`${folders} folder${folders === 1 ? '' : 's'}`)
  if (pages) parts.push(`${pages} page${pages === 1 ? '' : 's'}`)
  if (files) parts.push(`${files} file${files === 1 ? '' : 's'}`)
  return parts.join(' · ') || 'empty'
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}
