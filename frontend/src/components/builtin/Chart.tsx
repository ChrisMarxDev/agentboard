import { useData } from '../../hooks/useData'
import {
  BarChart, Bar, PieChart, Pie, Cell,
  XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
} from 'recharts'

interface ChartProps {
  source: string
  variant?: 'bar' | 'pie' | 'donut' | 'horizontal_bar'
}

const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316']

export function Chart({ source, variant = 'bar' }: ChartProps) {
  const { data, loading } = useData(source)

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
                <Cell key={i} fill={COLORS[i % COLORS.length]} />
              ))}
            </Pie>
            <Tooltip />
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
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
          {isHorizontal ? (
            <>
              <XAxis type="number" stroke="var(--text-secondary)" />
              <YAxis dataKey="name" type="category" stroke="var(--text-secondary)" width={100} />
            </>
          ) : (
            <>
              <XAxis dataKey="name" stroke="var(--text-secondary)" />
              <YAxis stroke="var(--text-secondary)" />
            </>
          )}
          <Tooltip />
          <Bar dataKey="value" fill="var(--accent)" radius={[4, 4, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
