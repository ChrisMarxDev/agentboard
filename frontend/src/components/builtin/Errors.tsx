import { useCallback, useEffect, useState } from 'react'

interface ErrorEntry {
  key: string
  component: string
  source?: string
  page?: string
  error: string
  first_seen: string
  last_seen: string
  count: number
}

interface ErrorsProps {
  limit?: number
}

/**
 * Renders the recent-errors buffer from `/api/errors`. Lives alongside the
 * other built-ins but talks directly to the errors endpoint instead of going
 * through a data key — the buffer has dedupe/count semantics that don't fit
 * the key-value shape.
 *
 * Refreshes on `agentboard:error-reported` / `agentboard:error-cleared` SSE
 * events so the display matches the server within ~100 ms.
 */
export function Errors({ limit = 10 }: ErrorsProps) {
  const [entries, setEntries] = useState<ErrorEntry[]>([])

  const refresh = useCallback(async () => {
    try {
      const r = await fetch('/api/errors')
      if (!r.ok) return
      const list = (await r.json()) as ErrorEntry[]
      setEntries(list)
    } catch {
      // silent — no beacon for the beacon viewer
    }
  }, [])

  useEffect(() => {
    refresh()
    const onReport = () => refresh()
    const onClear = () => refresh()
    window.addEventListener('agentboard:error-reported', onReport)
    window.addEventListener('agentboard:error-cleared', onClear)
    return () => {
      window.removeEventListener('agentboard:error-reported', onReport)
      window.removeEventListener('agentboard:error-cleared', onClear)
    }
  }, [refresh])

  const clearAll = async () => {
    await fetch('/api/errors', { method: 'DELETE' })
    // SSE will refresh, but optimistic clear keeps the UI responsive.
    setEntries([])
  }

  const clearOne = async (key: string) => {
    await fetch(`/api/errors?key=${encodeURIComponent(key)}`, { method: 'DELETE' })
    setEntries(es => es.filter(e => e.key !== key))
  }

  if (entries.length === 0) {
    return (
      <div
        className="p-3 rounded-md text-sm"
        style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)' }}
      >
        No render errors. ✓
      </div>
    )
  }

  const shown = entries.slice(0, limit)

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between text-xs" style={{ color: 'var(--text-secondary)' }}>
        <span>
          {entries.length} error{entries.length === 1 ? '' : 's'}
          {entries.length > limit ? ` · showing ${limit}` : ''}
        </span>
        <button
          onClick={clearAll}
          className="text-xs px-2 py-1 rounded"
          style={{
            background: 'transparent',
            border: '1px solid var(--border)',
            color: 'var(--text-secondary)',
            cursor: 'pointer',
          }}
        >
          Clear all
        </button>
      </div>
      {shown.map(e => (
        <div
          key={e.key}
          className="p-3 rounded-md text-xs"
          style={{
            background: 'var(--bg-secondary)',
            border: '1px solid var(--error)',
            color: 'var(--text)',
          }}
        >
          <div className="flex items-start justify-between gap-2 mb-1">
            <div className="flex items-center gap-2" style={{ fontWeight: 600 }}>
              <span style={{ color: 'var(--error)' }}>{e.component}</span>
              {e.count > 1 && (
                <span
                  className="px-1.5 rounded text-[10px]"
                  style={{ background: 'var(--border)', color: 'var(--text-secondary)' }}
                >
                  ×{e.count}
                </span>
              )}
              {e.page && (
                <span style={{ color: 'var(--text-secondary)', fontWeight: 400 }}>
                  on {e.page}
                </span>
              )}
              {e.source && (
                <span
                  className="px-1.5 rounded text-[10px]"
                  style={{ background: 'var(--border)', color: 'var(--text-secondary)', fontWeight: 400 }}
                >
                  {e.source}
                </span>
              )}
            </div>
            <button
              onClick={() => clearOne(e.key)}
              title="Clear this error"
              aria-label="Clear this error"
              style={{
                background: 'transparent',
                border: 'none',
                color: 'var(--text-secondary)',
                cursor: 'pointer',
                fontSize: '0.9rem',
                lineHeight: 1,
              }}
            >
              ×
            </button>
          </div>
          <pre
            className="font-mono whitespace-pre-wrap"
            style={{ color: 'var(--text-secondary)', fontSize: '0.75rem', lineHeight: 1.4, margin: 0 }}
          >
            {e.error}
          </pre>
        </div>
      ))}
    </div>
  )
}
