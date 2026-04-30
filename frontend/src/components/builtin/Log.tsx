import { useData } from '../../hooks/useData'
import { RichText } from './RichText'

interface LogProps {
  source: string
  limit?: number
}

const levelColors: Record<string, string> = {
  error: 'var(--error)',
  warn: 'var(--warning)',
  warning: 'var(--warning)',
  info: 'var(--accent)',
  debug: 'var(--text-secondary)',
}

const SYSTEM_KEYS = new Set(['_meta', '_id', 'id', 'ts', 'timestamp', 'level', 'message'])

// summarize folds the remaining (non-system) fields of an entry into a
// single readable line. Used when the entry has no `message` of its
// own — vet_activity rows like { actor, action, subject } get rendered
// as "Dr. Smith • completed checkup • Buddy (Golden Retriever)" instead
// of an empty stripe.
function summarize(entry: Record<string, unknown>): string {
  const parts: string[] = []
  for (const [k, v] of Object.entries(entry)) {
    if (SYSTEM_KEYS.has(k)) continue
    if (v == null) continue
    if (typeof v === 'object') {
      parts.push(`${k}: ${JSON.stringify(v)}`)
    } else {
      parts.push(String(v))
    }
  }
  return parts.join(' • ')
}

export function Log({ source, limit = 50 }: LogProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>Loading log...</div>
  }

  if (!data || !Array.isArray(data)) return null

  const entries = (data as Record<string, unknown>[]).slice(-limit).reverse()

  return (
    <div className="font-mono text-xs overflow-auto max-h-96">
      {entries.length === 0 ? (
        <div style={{ color: 'var(--text-secondary)' }}>No log entries</div>
      ) : (
        entries.map((entry, i) => {
          const level = String(entry.level ?? 'info').toLowerCase()
          const color = levelColors[level] ?? 'var(--text)'
          const tsRaw = entry.timestamp ?? entry.ts
          const message =
            entry.message != null && String(entry.message) !== ''
              ? String(entry.message)
              : summarize(entry)
          return (
            <div
              key={i}
              className="py-1 flex gap-3"
              style={{ borderBottom: '1px solid var(--border)' }}
            >
              {tsRaw != null && (
                <span style={{ color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                  {String(tsRaw)}
                </span>
              )}
              {entry.level != null && (
                <span style={{ color, fontWeight: 600, minWidth: 45, textTransform: 'uppercase' }}>
                  {String(entry.level)}
                </span>
              )}
              <span style={{ color: 'var(--text)' }}>
                <RichText text={message} />
              </span>
            </div>
          )
        })
      )}
    </div>
  )
}
