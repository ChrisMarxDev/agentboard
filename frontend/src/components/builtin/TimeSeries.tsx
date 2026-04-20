import { useMemo } from 'react'
import { useData } from '../../hooks/useData'
import { useResolvedTheme } from '../../hooks/useResolvedTheme'
import {
  LineChart, Line, BarChart, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts'

interface TimeSeriesProps {
  source: string
  variant?: 'line' | 'bar'
  x?: string
  y?: string
}

function cssVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

export function TimeSeries({ source, variant = 'line', x = 'x', y = 'y' }: TimeSeriesProps) {
  const { data, loading } = useData(source)
  const resolved = useResolvedTheme()

  const colors = useMemo(() => ({
    accent: cssVar('--accent', resolved === 'dark' ? '#60a5fa' : '#3b82f6'),
    grid:   cssVar('--border', resolved === 'dark' ? '#374151' : '#e5e7eb'),
    axis:   cssVar('--text-secondary', resolved === 'dark' ? '#9ca3af' : '#6b7280'),
    bg:     cssVar('--bg', resolved === 'dark' ? '#111827' : '#ffffff'),
    text:   cssVar('--text', resolved === 'dark' ? '#f9fafb' : '#111827'),
  }), [resolved])

  const tooltipStyle = {
    background: colors.bg,
    border: `1px solid ${colors.grid}`,
    borderRadius: '0.5rem',
    color: colors.text,
  }

  if (loading) {
    return <div className="h-64 flex items-center justify-center" style={{ color: 'var(--text-secondary)' }}>Loading...</div>
  }

  if (!data) return null

  let chartData: Record<string, unknown>[] = []

  if (Array.isArray(data)) {
    chartData = data as Record<string, unknown>[]
  } else if (typeof data === 'object') {
    const obj = data as { points?: Record<string, unknown>[]; x_field?: string; y_field?: string }
    chartData = obj.points ?? []
    if (obj.x_field) x = obj.x_field
    if (obj.y_field) y = obj.y_field
  }

  if (chartData.length === 0) {
    return <div className="p-4 text-sm" style={{ color: 'var(--text-secondary)' }}>No data points</div>
  }

  const ChartComponent = variant === 'bar' ? BarChart : LineChart

  return (
    <div className="my-4" style={{ width: '100%', height: 300 }}>
      <ResponsiveContainer>
        <ChartComponent data={chartData}>
          <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
          <XAxis dataKey={x} stroke={colors.axis} />
          <YAxis stroke={colors.axis} />
          <Tooltip contentStyle={tooltipStyle} cursor={{ stroke: colors.axis, strokeWidth: 1 }} />
          {variant === 'bar' ? (
            <Bar dataKey={y} fill={colors.accent} radius={[4, 4, 0, 0]} />
          ) : (
            <Line type="monotone" dataKey={y} stroke={colors.accent} strokeWidth={2} dot={false} />
          )}
        </ChartComponent>
      </ResponsiveContainer>
    </div>
  )
}
