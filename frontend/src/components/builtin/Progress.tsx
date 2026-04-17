import { useData } from '../../hooks/useData'

interface ProgressProps {
  source: string
  label?: string
}

export function Progress({ source, label }: ProgressProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="h-6 rounded" style={{ background: 'var(--bg-secondary)' }} />
  }

  let value = 0
  let max = 100
  let displayLabel = label

  if (data && typeof data === 'object' && !Array.isArray(data)) {
    const obj = data as Record<string, unknown>
    value = Number(obj.value ?? 0)
    max = Number(obj.max ?? 100)
    displayLabel = label ?? (obj.label as string | undefined)
  } else if (typeof data === 'number') {
    value = data
  }

  const percent = max > 0 ? Math.min(100, (value / max) * 100) : 0

  return (
    <div className="my-2">
      {displayLabel && (
        <div className="flex justify-between text-sm mb-1">
          <span style={{ color: 'var(--text-secondary)' }}>{displayLabel}</span>
          <span style={{ color: 'var(--text-secondary)' }}>{value}/{max}</span>
        </div>
      )}
      <div className="h-3 rounded-full overflow-hidden" style={{ background: 'var(--bg-secondary)' }}>
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{ width: `${percent}%`, background: 'var(--accent)' }}
        />
      </div>
    </div>
  )
}
