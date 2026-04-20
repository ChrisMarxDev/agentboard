import { useEffect, useRef, useState } from 'react'
import { useData } from '../../hooks/useData'

interface CounterProps {
  source: string
  label?: string
  format?: 'number' | 'currency' | 'percent'
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
    default:
      return num.toLocaleString()
  }
}

export function Counter({ source, label, format }: CounterProps) {
  const { data, loading } = useData(source)
  const [flash, setFlash] = useState<'up' | 'down' | null>(null)
  const previous = useRef<number | null>(null)

  const value = typeof data === 'number' ? data :
    (data && typeof data === 'object' && !Array.isArray(data))
      ? Number((data as Record<string, unknown>).value ?? 0)
      : Number(data)

  useEffect(() => {
    if (loading || isNaN(value)) return
    if (previous.current !== null && previous.current !== value) {
      setFlash(value > previous.current ? 'up' : 'down')
      const t = setTimeout(() => setFlash(null), 700)
      return () => clearTimeout(t)
    }
    previous.current = value
  }, [value, loading])

  useEffect(() => {
    if (!loading && !isNaN(value)) previous.current = value
  }, [value, loading])

  if (loading) return <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>Loading...</div>

  const color = flash === 'up' ? 'var(--success)' : flash === 'down' ? 'var(--error)' : 'var(--text)'

  return (
    <div>
      <div
        className="text-3xl font-bold"
        style={{
          color,
          transition: 'color 0.7s ease-out',
        }}
      >
        {formatValue(value, format)}
      </div>
      {label && (
        <div className="text-sm mt-1" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </div>
      )}
    </div>
  )
}
