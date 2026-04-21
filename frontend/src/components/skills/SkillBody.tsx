import { useEffect, useState, type ComponentType } from 'react'
import { compile, run } from '@mdx-js/mdx'
import * as runtime from 'react/jsx-runtime'

interface Props {
  source: string
}

/**
 * Compiles a markdown/MDX string (the SKILL.md body, frontmatter already
 * stripped) client-side and renders it. Matches the MDX pipeline used by
 * PageRenderer but without data or component bindings — skills are prose-only.
 */
export default function SkillBody({ source }: Props) {
  const [Content, setContent] = useState<ComponentType | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    if (!source.trim()) {
      setContent(null)
      setErr(null)
      return
    }
    let cancelled = false
    ;(async () => {
      try {
        const compiled = await compile(source, {
          outputFormat: 'function-body',
          development: false,
        })
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
          setErr(e instanceof Error ? e.message : 'Failed to render skill body')
        }
      }
    })()
    return () => { cancelled = true }
  }, [source])

  if (err) {
    return (
      <div
        className="p-3 rounded-md text-sm"
        style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--error)' }}
      >
        Skill body render error: {err}
      </div>
    )
  }
  if (!Content) return null

  const C = Content as ComponentType
  return (
    <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
      <C />
    </div>
  )
}
