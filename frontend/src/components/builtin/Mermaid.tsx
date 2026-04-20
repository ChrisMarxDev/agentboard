import { useEffect, useRef, useState } from 'react'
import { useData } from '../../hooks/useData'
import { useResolvedTheme } from '../../hooks/useResolvedTheme'

interface MermaidProps {
  source: string
  theme?: 'default' | 'dark' | 'forest' | 'neutral' | 'base'
}

// Stable per-instance id used by mermaid's render target.
let idCounter = 0
const nextId = () => `agentboard-mermaid-${++idCounter}`

export function Mermaid({ source, theme }: MermaidProps) {
  const { data, loading } = useData(source)
  const resolved = useResolvedTheme()
  const [svg, setSvg] = useState<string>('')
  const [err, setErr] = useState<string | null>(null)
  const idRef = useRef<string>(nextId())

  const code = typeof data === 'string' ? data :
    (data && typeof data === 'object' && 'code' in (data as object))
      ? String((data as Record<string, unknown>).code ?? '')
      : ''

  // Follow the sidebar theme switch; an explicit `theme` prop still wins.
  const mermaidTheme = theme ?? (resolved === 'dark' ? 'dark' : 'default')

  useEffect(() => {
    if (!code) {
      setSvg('')
      setErr(null)
      return
    }
    let cancelled = false
    ;(async () => {
      try {
        const mermaid = (await import('mermaid')).default
        mermaid.initialize({
          startOnLoad: false,
          theme: mermaidTheme,
          securityLevel: 'strict',
          fontFamily: 'inherit',
        })
        const { svg: rendered } = await mermaid.render(idRef.current, code)
        if (!cancelled) {
          setSvg(rendered)
          setErr(null)
        }
      } catch (e) {
        if (!cancelled) {
          setErr(e instanceof Error ? e.message : 'Failed to render diagram')
          setSvg('')
        }
      }
    })()
    return () => { cancelled = true }
  }, [code, mermaidTheme])

  if (loading) return null
  if (err) {
    return (
      <div
        className="p-3 rounded-md text-sm my-4"
        style={{
          background: 'var(--bg-secondary)',
          border: '1px solid var(--error)',
          color: 'var(--error)',
        }}
      >
        Mermaid error: {err}
      </div>
    )
  }
  if (!svg) return null

  return (
    <div
      className="my-4 overflow-x-auto"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}
