import { useData } from '../../hooks/useData'
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

export function TimeSeries({ source, variant = 'line', x = 'x', y = 'y' }: TimeSeriesProps) {
  const { data, loading } = useData(source)

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
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
          <XAxis dataKey={x} stroke="var(--text-secondary)" />
          <YAxis stroke="var(--text-secondary)" />
          <Tooltip />
          {variant === 'bar' ? (
            <Bar dataKey={y} fill="var(--accent)" radius={[4, 4, 0, 0]} />
          ) : (
            <Line type="monotone" dataKey={y} stroke="var(--accent)" strokeWidth={2} dot={false} />
          )}
        </ChartComponent>
      </ResponsiveContainer>
    </div>
  )
}
