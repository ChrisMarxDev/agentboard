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
import { PageMetaBar, type PageApprovalState } from './PageMetaBar'
import { SkillInstall } from '../builtin/SkillInstall'

// skillSlugFromPath detects pages that live under any folder literally
// named `skills/` and returns the slug immediately following it. Used
// to auto-mount <SkillInstall slug="..."/> above the page. Returns
// null if no skills/ segment is present, or if the segment is the last
// one (no slug to install).
function skillSlugFromPath(path: string): string | null {
  const parts = path.split('/').filter(Boolean)
  for (let i = 0; i < parts.length - 1; i++) {
    if (parts[i] === 'skills') return parts[i + 1]
  }
  return null
}

// sourceHasExplicitSkillInstall tells us whether the author already
// placed `<SkillInstall ... />` in their MDX — if so, we don't
// auto-mount a second card.
function sourceHasExplicitSkillInstall(source: string): boolean {
  return /<SkillInstall\b/.test(source)
}

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
  | {
      kind: 'page'
      Content: React.ComponentType
      title?: string
      source: string
      lastActor?: string
      lastAt?: string
      approval?: PageApprovalState | null
    }
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
  const bundle = dataContext.bundle

  // Compile the page source whenever the bundle changes. The view
  // broker returns a fresh bundle on every view/open; we just compile
  // what's already in hand — no direct /api/content fetch.
  const compileBundle = useCallback(async () => {
    if (!bundle || !bundle.source) return
    setLoading(true)
    setError(null)
    try {
      const firstHeading = bundle.source.match(/^#\s+(.+)$/m)
      const title = bundle.title ?? (firstHeading ? firstHeading[1].trim() : undefined)
      const compiled = await compile(bundle.source, {
        outputFormat: 'function-body',
        development: false,
        remarkPlugins: [remarkGfm],
      })
      const { default: MDXContent } = await run(String(compiled), {
        ...runtime,
        baseUrl: import.meta.url,
      })
      const approval: PageApprovalState | null = bundle.approval
        ? {
            approved_by: bundle.approval.approved_by,
            approved_at: bundle.approval.approved_at,
            approved_etag: bundle.approval.approved_etag,
            stale: bundle.approval.stale,
          }
        : null
      setResolved({
        kind: 'page',
        Content: MDXContent as React.ComponentType,
        title,
        source: bundle.source,
        lastActor: bundle.last_actor,
        lastAt: bundle.last_at,
        approval,
      })
    } catch (err) {
      console.error('Page render error:', err)
      setError(err instanceof Error ? err.message : 'Failed to render page')
    } finally {
      setLoading(false)
    }
  }, [bundle])

  // Once the view-broker fetch finishes without a bundle, we need to
  // probe whether this path is actually a file or a folder landing.
  const resolveFallback = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      // File probe.
      if (filePath) {
        const head = await apiFetch(`/api/files/${filePath}`, { method: 'HEAD' })
        if (head.ok) {
          setResolved({ kind: 'file' })
          setLoading(false)
          return
        }
      }
      // Folder landing.
      const folder = filePath ? findFolder(tree, filePath) : null
      if (folder) {
        setResolved({ kind: 'folder', folder })
        setLoading(false)
        return
      }
      setResolved({ kind: 'missing' })
    } catch (err) {
      console.error('Fallback resolve error:', err)
      setError(err instanceof Error ? err.message : 'Failed to resolve path')
    } finally {
      setLoading(false)
    }
  }, [filePath, tree])

  useEffect(() => {
    setResolved(null)
    setError(null)
    if (dataContext.loading) {
      setLoading(true)
      return
    }
    if (dataContext.error) {
      // view/open came back 401/403/etc — surface the friendly panel.
      setError('__AUTH__')
      setLoading(false)
      return
    }
    if (bundle && bundle.source) {
      void compileBundle()
      return
    }
    // No bundle → try file / folder fallback paths.
    void resolveFallback()
  }, [bundle, dataContext.loading, dataContext.error, compileBundle, resolveFallback])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64" style={{ color: 'var(--text-secondary)' }}>
        Loading...
      </div>
    )
  }

  if (error === '__AUTH__') {
    return <AuthRequiredPanel pagePath={pagePath} />
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
  const autoSkillSlug = skillSlugFromPath(pagePath)
  const autoSkillMount =
    autoSkillSlug && !sourceHasExplicitSkillInstall(resolved.source)
      ? autoSkillSlug
      : null

  return (
    <div className="relative">
      <PageActionsMenu pagePath={pagePath} pageTitle={resolved.title} />
      <PageMetaBar
        pagePath={pagePath}
        lastActor={resolved.lastActor}
        lastAt={resolved.lastAt}
        approval={resolved.approval ?? null}
        onApprovalChange={next =>
          setResolved(prev =>
            prev && prev.kind === 'page' ? { ...prev, approval: next } : prev,
          )
        }
      />
      {autoSkillMount && <SkillInstall slug={autoSkillMount} />}
      <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
        <Content components={components} data={dataContext.data} />
      </div>
    </div>
  )
}

// AuthRequiredPanel renders when a page fetch comes back 401/403.
// Shown to public-mode / share-link visitors who land on a path their
// token doesn't cover, and to signed-in users whose rules deny the
// page. Friendlier than "Failed to fetch: 401".
function AuthRequiredPanel({ pagePath }: { pagePath: string }) {
  const displayPath = pagePath === 'index' ? '/' : '/' + pagePath
  return (
    <div
      className="mx-auto max-w-lg my-16 text-center px-6 py-10 rounded-lg border"
      style={{
        background: 'var(--bg-secondary)',
        borderColor: 'var(--border)',
        color: 'var(--text)',
      }}
    >
      <div
        className="inline-flex items-center justify-center mb-4 rounded-full"
        style={{
          width: 48,
          height: 48,
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          color: 'var(--text-secondary)',
        }}
      >
        <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <rect width="18" height="11" x="3" y="11" rx="2" ry="2" />
          <path d="M7 11V7a5 5 0 0 1 10 0v4" />
        </svg>
      </div>
      <h2 className="text-lg font-semibold mb-2" style={{ color: 'var(--text)' }}>
        Sign in to view this page
      </h2>
      <p className="text-sm mb-6" style={{ color: 'var(--text-secondary)' }}>
        <code
          className="px-1 py-0.5 rounded"
          style={{ background: 'var(--bg)', border: '1px solid var(--border)' }}
        >
          {displayPath}
        </code>{' '}
        isn&apos;t in this board&apos;s public routes, so it needs a signed-in token to read.
      </p>
      <a
        href={'/login?next=' + encodeURIComponent(window.location.pathname + window.location.search)}
        className="inline-flex items-center gap-2 px-4 py-2 rounded-md text-sm font-medium"
        style={{ background: 'var(--accent)', color: 'white' }}
      >
        Sign in
      </a>
    </div>
  )
}
