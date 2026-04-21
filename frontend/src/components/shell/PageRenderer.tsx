import { useEffect, useState, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { compile, run } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'
import { useDataContext } from '../../hooks/DataContext'
import { getComponents } from '../../lib/componentRegistry'
import PageActionsMenu from './PageActionsMenu'

export default function PageRenderer() {
  const location = useLocation()
  const dataContext = useDataContext()
  const [content, setContent] = useState<React.ComponentType | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [pageTitle, setPageTitle] = useState<string | undefined>(undefined)

  const pagePath = location.pathname === '/' ? 'index' : location.pathname.slice(1)

  const loadPage = useCallback(async () => {
    setLoading(true)
    setError(null)

    try {
      // Fetch raw MDX source
      const resp = await fetch(`/api/content/${pagePath}`, {
        headers: { 'Accept': 'text/markdown' },
      })

      if (!resp.ok) {
        if (resp.status === 404) {
          setError('Page not found')
          setLoading(false)
          return
        }
        throw new Error(`Failed to fetch page: ${resp.status}`)
      }

      const source = await resp.text()

      const firstHeading = source.match(/^#\s+(.+)$/m)
      setPageTitle(firstHeading ? firstHeading[1].trim() : undefined)

      // Compile MDX client-side
      const compiled = await compile(source, {
        outputFormat: 'function-body',
        development: false,
      })

      // Run the compiled code
      const { default: MDXContent } = await run(String(compiled), {
        ...runtime,
        baseUrl: import.meta.url,
      })

      setContent(() => MDXContent)
    } catch (err) {
      console.error('Page render error:', err)
      setError(err instanceof Error ? err.message : 'Failed to render page')
    } finally {
      setLoading(false)
    }
  }, [pagePath])

  useEffect(() => {
    loadPage()
  }, [loadPage])

  // Listen for page updates via SSE
  useEffect(() => {
    const handler = () => loadPage()
    window.addEventListener('agentboard:page-updated', handler)
    return () => window.removeEventListener('agentboard:page-updated', handler)
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

  if (!content) return null

  const Content = content as React.ComponentType<{ components: Record<string, unknown>; data: Record<string, unknown> }>
  const components = getComponents()

  return (
    <div className="relative">
      <PageActionsMenu pagePath={pagePath} pageTitle={pageTitle} />
      <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
        <Content components={components} data={dataContext.data} />
      </div>
    </div>
  )
}
