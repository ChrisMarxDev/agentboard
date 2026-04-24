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
          return (
            <div
              key={i}
              className="py-1 flex gap-3"
              style={{ borderBottom: '1px solid var(--border)' }}
            >
              {entry.timestamp != null && (
                <span style={{ color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                  {String(entry.timestamp)}
                </span>
              )}
              {entry.level != null && (
                <span style={{ color, fontWeight: 600, minWidth: 45, textTransform: 'uppercase' }}>
                  {String(entry.level)}
                </span>
              )}
              <span style={{ color: 'var(--text)' }}>
                <RichText text={String(entry.message ?? '')} />
              </span>
            </div>
          )
        })
      )}
    </div>
  )
}
