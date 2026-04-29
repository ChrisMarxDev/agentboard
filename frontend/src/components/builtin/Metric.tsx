import { useData } from '../../hooks/useData'

interface MetricProps {
  // Inline: simplest and preferred for scalar values authored by hand.
  value?: number | string
  label?: string
  trend?: number
  comparison?: string | number
  format?: 'number' | 'currency' | 'percent' | 'duration'
  // Source-backed: pull from a frontmatter field on this page (`source="rev"`),
  // a folder collection (`source="tasks/"`), or a key on the files-first
  // store (`source="sales.q3"`). Updates from `agentboard_write` /
  // `PUT /api/data/<key>` rebroadcast over SSE to every subscriber.
  source?: string
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

export function Metric(props: MetricProps) {
  const { data, loading } = useData(props.source ?? '')

  if (props.source && loading) {
    return <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  let value: unknown = props.value
  let displayLabel = props.label
  let trend: unknown = props.trend
  let comparison: unknown = props.comparison

  if (value === undefined && props.source) {
    value = data
    if (data && typeof data === 'object' && !Array.isArray(data)) {
      const obj = data as Record<string, unknown>
      value = obj.value ?? data
      if (displayLabel === undefined) displayLabel = obj.label as string | undefined
      if (trend === undefined) trend = obj.trend
      if (comparison === undefined) comparison = obj.comparison
    }
  }

  return (
    <div>
      <div className="text-3xl font-bold" style={{ color: 'var(--text)' }}>
        {formatValue(value, props.format)}
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
