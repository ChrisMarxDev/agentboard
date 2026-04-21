import { useEffect, useState, type ComponentType } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { ArrowLeft, Download } from 'lucide-react'
import { compile, run } from '@mdx-js/mdx'
import remarkGfm from 'remark-gfm'
import * as runtime from 'react/jsx-runtime'

/**
 * Generic read-only file viewer for any path under `/files/*`. Picks a
 * rendering strategy from the response `Content-Type`:
 *
 *  - `text/markdown` | .md | .mdx → MDX compiled and rendered
 *  - starts with `image/` → inline <img>
 *  - starts with `text/` or looks textual → <pre>
 *  - otherwise → metadata card + download link
 *
 * Humans can inspect anything an agent uploaded; humans don't edit here.
 */
export default function FileViewer() {
  const { pathname } = useLocation()
  // After CORE_GUIDELINES §9 consolidation, non-markdown files live at
  // `/<path>` (no `/files/` prefix) and the PageRenderer delegates here on a
  // page 404 + file HEAD success. A legacy `/files/<path>` prefix is still
  // tolerated so old bookmarks keep working.
  const raw = pathname.startsWith('/files/') ? pathname.slice('/files/'.length) : pathname.slice(1)
  const filePath = decodeURI(raw)
  const apiUrl = `/api/files/${filePath}`

  const [meta, setMeta] = useState<{ status: number; contentType: string; size: number } | null>(null)
  const [body, setBody] = useState<string | null>(null)
  const [Content, setContent] = useState<ComponentType | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setMeta(null)
    setBody(null)
    setContent(null)
    setErr(null)

    ;(async () => {
      try {
        const r = await fetch(apiUrl)
        if (!r.ok) throw new Error(`${apiUrl} → ${r.status}`)
        const contentType = r.headers.get('Content-Type') || 'application/octet-stream'
        const size = Number(r.headers.get('Content-Length') || '0')

        if (cancelled) return
        setMeta({ status: r.status, contentType, size })

        const ext = filePath.split('.').pop()?.toLowerCase() || ''
        const isMarkdown = /markdown|mdx/.test(contentType) || ext === 'md' || ext === 'mdx'
        const isText = contentType.startsWith('text/') || /json|javascript|xml/.test(contentType)
        const isImage = contentType.startsWith('image/')

        if (isImage) {
          return // no body read needed; <img> uses the URL
        }

        if (isMarkdown || isText || ext === 'json') {
          const text = await r.text()
          if (cancelled) return
          setBody(text)

          if (isMarkdown) {
            try {
              const compiled = await compile(stripFrontmatter(text), {
                outputFormat: 'function-body',
                development: false,
                remarkPlugins: [remarkGfm],
              })
              const { default: MDXContent } = await run(String(compiled), {
                ...runtime,
                baseUrl: import.meta.url,
              })
              if (!cancelled) setContent(() => MDXContent)
            } catch (e) {
              if (!cancelled) {
                // fall back to raw text; MDX compile failure just means we show <pre>
                setErr(e instanceof Error ? e.message : 'MDX compile failed')
              }
            }
          }
        }
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : 'Failed to load file')
      }
    })()

    return () => {
      cancelled = true
    }
  }, [apiUrl, filePath])

  const parentDir = filePath.includes('/') ? filePath.slice(0, filePath.lastIndexOf('/')) : ''

  const Header = (
    <header className="mb-4 flex items-start justify-between gap-4">
      <div className="min-w-0">
        <div className="text-xs mb-1" style={{ color: 'var(--text-secondary)' }}>
          {parentDir && <span>{parentDir}/</span>}
        </div>
        <h1
          className="text-xl font-semibold truncate"
          style={{ color: 'var(--text)' }}
          title={filePath}
        >
          {filePath.split('/').pop()}
        </h1>
        {meta && (
          <div className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
            {meta.contentType} · {formatSize(meta.size)}
          </div>
        )}
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <a
          href={apiUrl}
          download
          title="Download"
          className="h-8 w-8 flex items-center justify-center rounded-md"
          style={{
            background: 'var(--bg)',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
          }}
        >
          <Download size={14} />
        </a>
      </div>
    </header>
  )

  if (err && !body && !Content) {
    return (
      <div>
        <BackLink />
        {Header}
        <div
          className="p-3 rounded-md text-sm"
          style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--error)' }}
        >
          {err}
        </div>
      </div>
    )
  }

  // Image
  if (meta?.contentType.startsWith('image/')) {
    return (
      <div>
        <BackLink />
        {Header}
        <div
          className="p-4 rounded-md flex items-center justify-center"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
        >
          <img
            src={apiUrl}
            alt={filePath}
            style={{ maxWidth: '100%', maxHeight: '70vh', display: 'block' }}
          />
        </div>
      </div>
    )
  }

  // Markdown — prefer rendered when compile succeeded
  if (Content) {
    const C = Content
    return (
      <div>
        <BackLink />
        {Header}
        <div className="prose prose-sm max-w-none dark:prose-invert mdx-content">
          <C />
        </div>
      </div>
    )
  }

  // Text / JSON / markdown-fallback
  if (body != null) {
    return (
      <div>
        <BackLink />
        {Header}
        <pre
          className="p-3 rounded-md text-xs overflow-x-auto"
          style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--border)',
            color: 'var(--text)',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
            fontFamily:
              'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
          }}
        >
          {body}
        </pre>
      </div>
    )
  }

  // Binary fallback
  if (meta) {
    return (
      <div>
        <BackLink />
        {Header}
        <div
          className="p-4 rounded-md text-sm"
          style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)', color: 'var(--text-secondary)' }}
        >
          Binary file — preview not available. Use the download button above.
        </div>
      </div>
    )
  }

  return (
    <div style={{ color: 'var(--text-secondary)' }} className="text-sm">
      Loading…
    </div>
  )
}

function BackLink() {
  return (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm mb-2"
      style={{ color: 'var(--text-secondary)' }}
    >
      <ArrowLeft size={14} /> Back
    </Link>
  )
}

// Strip a leading YAML frontmatter block so the rendered markdown doesn't
// show the `---` wrapper as inline text. Any other `---` farther down is
// left alone.
function stripFrontmatter(text: string): string {
  if (!text.startsWith('---\n') && !text.startsWith('---\r\n')) return text
  const close = text.indexOf('\n---', 4)
  if (close < 0) return text
  const after = text.indexOf('\n', close + 4)
  return after < 0 ? '' : text.slice(after + 1)
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(2)} MB`
}
