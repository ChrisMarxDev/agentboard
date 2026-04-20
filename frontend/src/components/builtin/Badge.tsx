import { useData } from '../../hooks/useData'
import type { CSSProperties } from 'react'

type BadgeVariant = 'default' | 'accent' | 'success' | 'warning' | 'error'

interface BadgeProps {
  source: string
  variant?: BadgeVariant
}

const variantStyles: Record<BadgeVariant, { bg: string; fg: string }> = {
  default: { bg: 'var(--bg-secondary)', fg: 'var(--text-secondary)' },
  accent:  { bg: 'var(--accent-light)', fg: 'var(--accent)' },
  success: { bg: 'rgba(16,185,129,0.15)', fg: 'var(--success)' },
  warning: { bg: 'rgba(245,158,11,0.15)', fg: 'var(--warning)' },
  error:   { bg: 'rgba(239,68,68,0.15)',  fg: 'var(--error)' },
}

export function Badge({ source, variant }: BadgeProps) {
  const { data, loading } = useData(source)
  if (loading) return null
  if (data == null) return null

  let text: string
  let v: BadgeVariant = variant ?? 'default'

  if (typeof data === 'object' && !Array.isArray(data)) {
    const obj = data as Record<string, unknown>
    text = String(obj.text ?? obj.label ?? '')
    if (!variant && typeof obj.variant === 'string' && obj.variant in variantStyles) {
      v = obj.variant as BadgeVariant
    }
  } else {
    text = String(data)
  }

  if (!text) return null

  const styles = variantStyles[v]
  const style: CSSProperties = {
    display: 'inline-block',
    padding: '0.15rem 0.6rem',
    fontSize: '0.75rem',
    fontWeight: 600,
    borderRadius: '9999px',
    background: styles.bg,
    color: styles.fg,
    lineHeight: 1.4,
    verticalAlign: 'middle',
  }

  return <span style={style}>{text}</span>
}
