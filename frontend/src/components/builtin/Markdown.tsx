import { useEffect, useState, type ComponentType } from 'react'
import { compile, run } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'
import { useData } from '../../hooks/useData'

interface MarkdownProps {
  source: string
}

export function Markdown({ source }: MarkdownProps) {
  const { data, loading } = useData(source)
  const [Content, setContent] = useState<ComponentType | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const text = typeof data === 'string' ? data :
    (data && typeof data === 'object' && 'text' in (data as object))
      ? String((data as Record<string, unknown>).text)
      : ''

  useEffect(() => {
    if (!text) {
      setContent(null)
      setErr(null)
      return
    }
    let cancelled = false
    ;(async () => {
      try {
        const compiled = await compile(text, { outputFormat: 'function-body', development: false })
        const { default: MDXContent } = await run(String(compiled), {
          ...runtime,
          baseUrl: import.meta.url,
        })
        if (!cancelled) {
          setContent(() => MDXContent)
          setErr(null)
        }
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : 'Failed to render markdown')
      }
    })()
    return () => { cancelled = true }
  }, [text])

  if (loading) return null
  if (err) return <div style={{ color: 'var(--error)', fontSize: '0.875rem' }}>Markdown error: {err}</div>
  if (!Content) return null

  const C = Content as ComponentType
  return <div className="markdown-live"><C /></div>
}
