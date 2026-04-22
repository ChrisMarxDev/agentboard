import { useEffect, useMemo, useState, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { compile, run } from '@mdx-js/mdx'
import remarkGfm from 'remark-gfm'
import * as runtime from 'react/jsx-runtime'
import { useDataContext } from '../../hooks/DataContext'
import { apiFetch } from '../../lib/session'
import { getComponents } from '../../lib/componentRegistry'
import PageActionsMenu from './PageActionsMenu'
import FileViewer from '../files/FileViewer'
import FolderView from './FolderView'
import { GrabbableHeading } from './GrabbableHeading'
import { MissingBrick } from './MissingBrick'
import { LastEditedFooter } from './LastEditedFooter'

// Cache MissingBrick stand-ins per-name at the module level so React sees a
// stable component reference across renders. Without this, each render
// produced a fresh arrow function and React's reconciler blew up.
const missingBrickCache = new Map<string, React.ComponentType>()
function getMissingBrick(name: string): React.ComponentType {
  let C = missingBrickCache.get(name)
  if (!C) {
    C = () => <MissingBrick name={name} />
    C.displayName = `MissingBrick(${name})`
    missingBrickCache.set(name, C)
  }
  return C
}

// buildComponentMap assembles the full component map MDX receives. It
// starts from the registry + grabbable heading overrides, then scans the
// page source for unknown uppercase JSX tags and fills them with
// MissingBrick stand-ins. The spread MDX runs internally then carries the
// placeholders through without losing them (a Proxy wouldn't survive the
// spread).
function buildComponentMap(source: string): Record<string, unknown> {
  const base: Record<string, unknown> = {
    ...getComponents(),
    h1: (p: { children?: React.ReactNode }) => <GrabbableHeading level={1}>{p.children}</GrabbableHeading>,
    h2: (p: { children?: React.ReactNode }) => <GrabbableHeading level={2}>{p.children}</GrabbableHeading>,
    h3: (p: { children?: React.ReactNode }) => <GrabbableHeading level={3}>{p.children}</GrabbableHeading>,
  }
  // Scan for `<Foo ...>` or `<Foo/>` where Foo is PascalCase (JSX component).
  // Conservative regex: lookbehind-free so it works in Safari.
  const unknown = new Set<string>()
  const re = /<([A-Z][A-Za-z0-9]*)\b/g
  let match: RegExpExecArray | null
  while ((match = re.exec(source)) !== null) {
    const name = match[1]
    if (!(name in base)) unknown.add(name)
  }
  for (const name of unknown) {
    base[name] = getMissingBrick(name)
  }
  return base
}
import { usePages } from '../../hooks/usePages'
import { useFiles } from '../../hooks/useFiles'
import { buildContentTree, findFolder, type ContentFolder } from '../../lib/contentTree'

type Resolved =
  | { kind: 'page'; Content: React.ComponentType; title?: string; source: string; lastActor?: string; lastAt?: string }
  | { kind: 'file' }
  | { kind: 'folder'; folder: ContentFolder }
  | { kind: 'missing' }

export default function PageRenderer() {
  const location = useLocation()
  const dataContext = useDataContext()
  const pages = usePages()
  const { files } = useFiles()
  const tree = useMemo(() => buildContentTree(pages, files), [pages, files])
  const [resolved, setResolved] = useState<Resolved | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const pagePath = location.pathname === '/' ? 'index' : location.pathname.slice(1)
  const filePath = location.pathname.slice(1)

  const loadPage = useCallback(async () => {
    setLoading(true)
    setError(null)
    setResolved(null)

    // Try page first. If not a page, try file.
    try {
      const pageResp = await apiFetch(`/api/content/${pagePath}`, {
        headers: { Accept: 'text/markdown' },
      })

      if (pageResp.ok) {
        const source = await pageResp.text()
        const firstHeading = source.match(/^#\s+(.+)$/m)
        const title = firstHeading ? firstHeading[1].trim() : undefined
        const lastActor = pageResp.headers.get('X-Last-Actor') ?? undefined
        const lastAt = pageResp.headers.get('X-Last-At') ?? undefined

        const compiled = await compile(source, {
          outputFormat: 'function-body',
          development: false,
          remarkPlugins: [remarkGfm],
        })
        const { default: MDXContent } = await run(String(compiled), {
          ...runtime,
          baseUrl: import.meta.url,
        })
        setResolved({
          kind: 'page',
          Content: MDXContent as React.ComponentType,
          title,
          source,
          lastActor,
          lastAt,
        })
        setLoading(false)
        return
      }

      if (pageResp.status !== 404) {
        throw new Error(`Failed to fetch page: ${pageResp.status}`)
      }

      // 404 from pages: maybe it's a file at exactly this path.
      if (filePath) {
        const head = await apiFetch(`/api/files/${filePath}`, { method: 'HEAD' })
        if (head.ok) {
          setResolved({ kind: 'file' })
          setLoading(false)
          return
        }
      }

      // Neither a page nor a file — could still be a known folder prefix.
      // Render a generated folder landing view (CORE_GUIDELINES §9).
      const folder = filePath ? findFolder(tree, filePath) : null
      if (folder) {
        setResolved({ kind: 'folder', folder })
        setLoading(false)
        return
      }

      setResolved({ kind: 'missing' })
    } catch (err) {
      console.error('Page render error:', err)
      setError(err instanceof Error ? err.message : 'Failed to render page')
    } finally {
      setLoading(false)
    }
  }, [pagePath, filePath, tree])

  useEffect(() => {
    loadPage()
  }, [loadPage])

  useEffect(() => {
    const handler = () => loadPage()
    window.addEventListener('agentboard:page-updated', handler)
    window.addEventListener('agentboard:file-updated', handler)
    return () => {
      window.removeEventListener('agentboard:page-updated', handler)
      window.removeEventListener('agentboard:file-updated', handler)
    }
  }, [loadPage])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64" style={{ color: 'var(--text-secondary)' }}>
        Loading...
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-4 rounded-md" style={{ background: 'var(--bg-secondary)', color: 'var(--error)' }}>
        {error}
      </div>
    )
  }

  if (!resolved) return null

  if (resolved.kind === 'file') {
    return <FileViewer />
  }

  if (resolved.kind === 'folder') {
    return <FolderView folder={resolved.folder} />
  }

  if (resolved.kind === 'missing') {
    return (
      <div className="p-4 rounded-md" style={{ background: 'var(--bg-secondary)', color: 'var(--error)' }}>
        Not found: {location.pathname}
      </div>
    )
  }

  const Content = resolved.Content as React.ComponentType<{
    components: Record<string, unknown>
    data: Record<string, unknown>
  }>
  const components = buildComponentMap(resolved.source)

  return (
    <div className="relative">
      <PageActionsMenu pagePath={pagePath} pageTitle={resolved.title} />
      <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
        <Content components={components} data={dataContext.data} />
      </div>
      <LastEditedFooter actor={resolved.lastActor} at={resolved.lastAt} />
    </div>
  )
}
