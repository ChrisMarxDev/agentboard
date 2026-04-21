import { useCallback, useEffect, useState } from 'react'
import { Download } from 'lucide-react'

interface ApiListProps {
  src: string
  titleKey?: string
  descriptionKey?: string
  idKey?: string
  downloadPrefix?: string
  empty?: string
  refreshOn?: string
}

type Row = Record<string, unknown>

const TITLE_FALLBACKS = ['title', 'name', 'slug', 'id', 'key']
const DESC_FALLBACKS = ['description', 'summary', 'detail']
const ID_FALLBACKS = ['slug', 'id', 'key', 'name']

function pick(row: Row, preferred: string | undefined, fallbacks: string[]): string {
  if (preferred && row[preferred] != null) return String(row[preferred])
  for (const k of fallbacks) if (row[k] != null) return String(row[k])
  return ''
}

/**
 * Generic list renderer for any REST endpoint that returns a JSON array of
 * objects. Used to surface built-in collections (skills, errors, pages, …) on
 * authored pages without adding type-specific React routes.
 */
export function ApiList({
  src,
  titleKey,
  descriptionKey,
  idKey,
  downloadPrefix,
  empty = 'Nothing here yet.',
  refreshOn,
}: ApiListProps) {
  const [rows, setRows] = useState<Row[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await fetch(src)
      if (!r.ok) throw new Error(`${src} → ${r.status}`)
      const data = await r.json()
      setRows(Array.isArray(data) ? (data as Row[]) : [])
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load')
    } finally {
      setLoaded(true)
    }
  }, [src])

  useEffect(() => {
    load()
    if (!refreshOn) return
    const on = () => load()
    window.addEventListener(refreshOn, on)
    return () => window.removeEventListener(refreshOn, on)
  }, [load, refreshOn])

  if (error) {
    return (
      <div
        className="p-3 rounded-md text-sm"
        style={{ background: 'rgba(239,68,68,0.08)', color: 'var(--error)' }}
      >
        {error}
      </div>
    )
  }

  if (loaded && rows.length === 0) {
    return (
      <div
        className="p-3 rounded-md text-sm"
        style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)' }}
      >
        {empty}
      </div>
    )
  }

  return (
    <ul className="flex flex-col gap-2">
      {rows.map((row, i) => {
        const title = pick(row, titleKey, TITLE_FALLBACKS)
        const desc = pick(row, descriptionKey, DESC_FALLBACKS)
        const id = pick(row, idKey, ID_FALLBACKS)
        return (
          <li
            key={id || i}
            className="rounded-md p-3 flex items-start justify-between gap-4"
            style={{ background: 'var(--bg-secondary)', border: '1px solid var(--border)' }}
          >
            <div className="min-w-0 flex-1">
              <div className="font-medium" style={{ color: 'var(--text)' }}>
                {title || id || '(untitled)'}
              </div>
              {desc && (
                <div className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
                  {desc}
                </div>
              )}
            </div>
            {downloadPrefix && id && (
              <a
                href={`${downloadPrefix}${encodeURIComponent(id)}`}
                title="Download"
                aria-label={`Download ${title || id}`}
                className="h-8 w-8 flex items-center justify-center rounded-md shrink-0"
                style={{
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  color: 'var(--text-secondary)',
                }}
              >
                <Download size={14} />
              </a>
            )}
          </li>
        )
      })}
    </ul>
  )
}
