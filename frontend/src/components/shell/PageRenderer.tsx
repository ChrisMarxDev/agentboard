import { useEffect, useState, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { compile, run } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'
import { useDataContext } from '../../hooks/DataContext'
import { getComponents } from '../../lib/componentRegistry'
import PageActionsMenu from './PageActionsMenu'
import FileViewer from '../files/FileViewer'

type Resolved =
  | { kind: 'page'; Content: React.ComponentType; title?: string }
  | { kind: 'file' }
  | { kind: 'missing' }

export default function PageRenderer() {
  const location = useLocation()
  const dataContext = useDataContext()
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
      const pageResp = await fetch(`/api/content/${pagePath}`, {
        headers: { Accept: 'text/markdown' },
      })

      if (pageResp.ok) {
        const source = await pageResp.text()
        const firstHeading = source.match(/^#\s+(.+)$/m)
        const title = firstHeading ? firstHeading[1].trim() : undefined

        const compiled = await compile(source, {
          outputFormat: 'function-body',
          development: false,
        })
        const { default: MDXContent } = await run(String(compiled), {
          ...runtime,
          baseUrl: import.meta.url,
        })
        setResolved({ kind: 'page', Content: MDXContent as React.ComponentType, title })
        setLoading(false)
        return
      }

      if (pageResp.status !== 404) {
        throw new Error(`Failed to fetch page: ${pageResp.status}`)
      }

      // 404 from pages: maybe it's a file at exactly this path.
      if (filePath) {
        const head = await fetch(`/api/files/${filePath}`, { method: 'HEAD' })
        if (head.ok) {
          setResolved({ kind: 'file' })
          setLoading(false)
          return
        }
      }

      setResolved({ kind: 'missing' })
    } catch (err) {
      console.error('Page render error:', err)
      setError(err instanceof Error ? err.message : 'Failed to render page')
    } finally {
      setLoading(false)
    }
  }, [pagePath, filePath])

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
  const components = getComponents()

  return (
    <div className="relative">
      <PageActionsMenu pagePath={pagePath} pageTitle={resolved.title} />
      <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
        <Content components={components} data={dataContext.data} />
      </div>
    </div>
  )
}
