import { useEffect, useState, type ComponentType, type ReactNode } from 'react'
import { compile, run } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'
import { useData } from '../../hooks/useData'
import { beaconError, resetBeacon } from '../../lib/errorBeacon'

interface MarkdownProps {
  text?: string
  children?: ReactNode
  source?: string
}

export function Markdown({ text, children, source }: MarkdownProps) {
  const { data, loading } = useData(source ?? '')
  const [Content, setContent] = useState<ComponentType | null>(null)
  const [err, setErr] = useState<string | null>(null)

  // Inline children are already rendered MDX — no recompile needed. Only the
  // `text` prop and `source`-backed strings go through the compile pipeline.
  const inline = text !== undefined
    ? text
    : undefined

  const kvText = !inline && source
    ? (typeof data === 'string' ? data :
       (data && typeof data === 'object' && 'text' in (data as object))
         ? String((data as Record<string, unknown>).text)
         : '')
    : ''

  const textToCompile = inline ?? kvText

  useEffect(() => {
    if (!textToCompile) {
      setContent(null)
      setErr(null)
      return
    }
    let cancelled = false
    ;(async () => {
      try {
        const compiled = await compile(textToCompile, { outputFormat: 'function-body', development: false })
        const { default: MDXContent } = await run(String(compiled), {
          ...runtime,
          baseUrl: import.meta.url,
        })
        if (!cancelled) {
          setContent(() => MDXContent)
          setErr(null)
        }
      } catch (e) {
        if (!cancelled) {
          const msg = e instanceof Error ? e.message : 'Failed to render markdown'
          setErr(msg)
          beaconError({ component: 'Markdown', source: source ?? '(inline)', error: msg })
        }
      }
    })()
    return () => { cancelled = true }
  }, [textToCompile, source])

  useEffect(() => { resetBeacon('Markdown', source ?? '(inline)') }, [textToCompile, source])

  // If children were passed (not text/source), render them as-is — they're
  // already compiled by the parent MDX.
  if (children !== undefined && text === undefined && !source) {
    return <div className="markdown-live">{children}</div>
  }

  if (source && loading) return null
  if (err) return <div style={{ color: 'var(--error)', fontSize: '0.875rem' }}>Markdown error: {err}</div>
  if (!Content) return null

  const C = Content as ComponentType
  return <div className="markdown-live"><C /></div>
}
