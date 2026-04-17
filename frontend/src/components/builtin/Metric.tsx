import { useData } from '../../hooks/useData'

interface MetricProps {
  source: string
  label?: string
  format?: 'number' | 'currency' | 'percent' | 'duration'
}

function formatValue(value: unknown, format?: string): string {
  if (value === null || value === undefined) return '—'
  const num = typeof value === 'number' ? value : Number(value)
  if (isNaN(num)) return String(value)

  switch (format) {
    case 'currency':
      return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(num)
    case 'percent':
      return new Intl.NumberFormat('en-US', { style: 'percent', minimumFractionDigits: 1 }).format(num / 100)
    case 'duration': {
      const h = Math.floor(num / 3600)
      const m = Math.floor((num % 3600) / 60)
      const s = num % 60
      return h > 0 ? `${h}h ${m}m` : m > 0 ? `${m}m ${s}s` : `${s}s`
    }
    default:
      return num.toLocaleString()
  }
}

export function Metric({ source, label, format }: MetricProps) {
  const { data, loading } = useData(source)

  if (loading) {
    return <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  let value: unknown = data
  let displayLabel = label
  let trend: unknown = undefined
  let comparison: unknown = undefined

  if (data && typeof data === 'object' && !Array.isArray(data)) {
    const obj = data as Record<string, unknown>
    value = obj.value ?? data
    displayLabel = label ?? (obj.label as string | undefined)
    trend = obj.trend
    comparison = obj.comparison
  }

  return (
    <div>
      <div className="text-3xl font-bold" style={{ color: 'var(--text)' }}>
        {formatValue(value, format)}
      </div>
      {displayLabel && (
        <div className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
          {displayLabel}
        </div>
      )}
      {trend !== undefined && (
        <div className="text-xs mt-1" style={{ color: Number(trend) >= 0 ? 'var(--success)' : 'var(--error)' }}>
          {Number(trend) >= 0 ? '↑' : '↓'} {Math.abs(Number(trend))}%
          {comparison != null && <span style={{ color: 'var(--text-secondary)' }}> vs {String(comparison)}</span>}
        </div>
      )}
    </div>
  )
}
