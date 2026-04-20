import { useMemo } from 'react'
import { useData } from '../../hooks/useData'
import { useResolvedTheme } from '../../hooks/useResolvedTheme'
import {
  BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts'

interface ChartProps {
  source: string
  variant?: 'bar' | 'pie' | 'donut' | 'horizontal_bar'
}

// Two palettes for multi-series pie/donut charts — light mode uses saturated
// colors, dark mode uses slightly lighter/desaturated so they read on dark bg.
const LIGHT_PALETTE = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316']
const DARK_PALETTE  = ['#60a5fa', '#34d399', '#fbbf24', '#f87171', '#a78bfa', '#f472b6', '#2dd4bf', '#fb923c']

// Resolve a CSS custom property to its actual hex/rgb string. Recharts sometimes
// doesn't honor `var()` in SVG attributes (pie fills, tooltip borders), so we
// materialize the value when passing it down.
function cssVar(name: string, fallback: string): string {
  if (typeof document === 'undefined') return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

export function Chart({ source, variant = 'bar' }: ChartProps) {
  const { data, loading } = useData(source)
  const resolved = useResolvedTheme()

  // Re-evaluate colors whenever the theme flips.
  const colors = useMemo(() => {
    const palette = resolved === 'dark' ? DARK_PALETTE : LIGHT_PALETTE
    return {
      palette,
      accent: cssVar('--accent', palette[0]),
      grid:   cssVar('--border', resolved === 'dark' ? '#374151' : '#e5e7eb'),
      axis:   cssVar('--text-secondary', resolved === 'dark' ? '#9ca3af' : '#6b7280'),
      bg:     cssVar('--bg', resolved === 'dark' ? '#111827' : '#ffffff'),
      text:   cssVar('--text', resolved === 'dark' ? '#f9fafb' : '#111827'),
    }
  }, [resolved])

  const tooltipStyle = {
    background: colors.bg,
    border: `1px solid ${colors.grid}`,
    borderRadius: '0.5rem',
    color: colors.text,
  }

  if (loading) {
    return <div className="h-64 flex items-center justify-center" style={{ color: 'var(--text-secondary)' }}>Loading chart...</div>
  }

  if (!data) return null

  let chartData: { name: string; value: number }[] = []

  if (Array.isArray(data)) {
    chartData = (data as Record<string, unknown>[]).map(item => ({
      name: String(item.name ?? item.label ?? item.category ?? ''),
      value: Number(item.value ?? item.count ?? 0),
    }))
  } else if (typeof data === 'object') {
    const obj = data as { labels?: string[]; values?: number[] }
    if (obj.labels && obj.values) {
      chartData = obj.labels.map((label, i) => ({
        name: label,
        value: obj.values![i] ?? 0,
      }))
    }
  }

  if (chartData.length === 0) return null

  if (variant === 'pie' || variant === 'donut') {
    return (
      <div className="my-4" style={{ width: '100%', height: 300 }}>
        <ResponsiveContainer>
          <PieChart>
            <Pie
              data={chartData}
              cx="50%"
              cy="50%"
              innerRadius={variant === 'donut' ? 60 : 0}
              outerRadius={100}
              dataKey="value"
              label={({ name, percent }: { name?: string; percent?: number }) => `${name ?? ''} ${((percent ?? 0) * 100).toFixed(0)}%`}
            >
              {chartData.map((_, i) => (
                <Cell key={i} fill={colors.palette[i % colors.palette.length]} />
              ))}
            </Pie>
            <Tooltip contentStyle={tooltipStyle} />
          </PieChart>
        </ResponsiveContainer>
      </div>
    )
  }

  const isHorizontal = variant === 'horizontal_bar'

  return (
    <div className="my-4" style={{ width: '100%', height: 300 }}>
      <ResponsiveContainer>
        <BarChart data={chartData} layout={isHorizontal ? 'vertical' : 'horizontal'}>
          <CartesianGrid strokeDasharray="3 3" stroke={colors.grid} />
          {isHorizontal ? (
            <>
              <XAxis type="number" stroke={colors.axis} />
              <YAxis dataKey="name" type="category" stroke={colors.axis} width={100} />
            </>
          ) : (
            <>
              <XAxis dataKey="name" stroke={colors.axis} />
              <YAxis stroke={colors.axis} />
            </>
          )}
          <Tooltip contentStyle={tooltipStyle} cursor={{ fill: colors.grid, opacity: 0.3 }} />
          <Bar dataKey="value" fill={colors.accent} radius={[4, 4, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
